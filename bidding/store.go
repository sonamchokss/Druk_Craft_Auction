package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/db"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
)

const schema = `
CREATE TABLE IF NOT EXISTS auctions (
    auction_id        TEXT PRIMARY KEY,
    item_title        TEXT        NOT NULL DEFAULT '',
    artisan_id        TEXT        NOT NULL DEFAULT '',
    start_price       BIGINT      NOT NULL DEFAULT 0,
    status            TEXT        NOT NULL,
    ends_at           TIMESTAMPTZ,
    highest_bid_id    TEXT,
    highest_bidder_id TEXT,
    highest_amount    BIGINT,
    winner_id         TEXT,
    final_price       BIGINT,
    opened_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at         TIMESTAMPTZ
);

CREATE SEQUENCE IF NOT EXISTS bid_seq START 1001;

CREATE TABLE IF NOT EXISTS bids (
    bid_id     TEXT PRIMARY KEY DEFAULT ('B-' || nextval('bid_seq')),
    auction_id TEXT        NOT NULL REFERENCES auctions(auction_id),
    bidder_id  TEXT        NOT NULL,
    amount     BIGINT      NOT NULL,
    placed_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS bids_auction_idx ON bids (auction_id, amount DESC);
CREATE INDEX IF NOT EXISTS bids_bidder_idx  ON bids (bidder_id);

-- Transactional outbox: BidPlaced is written here in the same transaction as
-- the bid, then published by the relay (outbox.go).
CREATE TABLE IF NOT EXISTS outbox (
    seq          BIGSERIAL PRIMARY KEY,
    event_id     UUID        NOT NULL UNIQUE,
    envelope     JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox (seq) WHERE published_at IS NULL;
` + db.ProcessedEventsDDL

type Store struct {
	pool *pgxpool.Pool
}

type Bid struct {
	BidID     string    `json:"bidId"`
	AuctionID string    `json:"auctionId"`
	BidderID  string    `json:"bidderId"`
	Amount    int64     `json:"amount"`
	PlacedAt  time.Time `json:"placedAt"`
}

type AuctionBids struct {
	AuctionID       string     `json:"auctionId"`
	ItemTitle       string     `json:"itemTitle"`
	Status          string     `json:"status"`
	StartPrice      int64      `json:"startPrice"`
	EndsAt          *time.Time `json:"endsAt"`
	HighestAmount   *int64     `json:"highestAmount"`
	HighestBidderID *string    `json:"highestBidderId"`
	WinnerID        *string    `json:"winnerId"`
	Bids            []Bid      `json:"bids"`
}

type BidderBid struct {
	Bid
	ItemTitle     string `json:"itemTitle"`
	AuctionStatus string `json:"auctionStatus"`
	IsHighest     bool   `json:"isHighest"`
}

var errAuctionNotFound = &bidError{http.StatusNotFound, "Auction not found or not open yet"}

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// PlaceBid validates and saves a bid, and queues its BidPlaced event in the
// outbox, all in one transaction. A rejected bid returns a *bidError and
// writes nothing — so no event is published (test T10).
func (s *Store) PlaceBid(ctx context.Context, auctionID, bidderID string, amount int64) (Bid, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Bid{}, err
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE serialises concurrent bids on the same auction.
	var a auctionState
	err = tx.QueryRow(ctx, `
		SELECT status, artisan_id, start_price, highest_amount, highest_bidder_id
		FROM auctions WHERE auction_id = $1 FOR UPDATE`, auctionID).
		Scan(&a.Status, &a.ArtisanID, &a.StartPrice, &a.HighestAmount, &a.HighestBidderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bid{}, errAuctionNotFound
	}
	if err != nil {
		return Bid{}, err
	}
	if berr := validateBid(a, bidderID, amount); berr != nil {
		return Bid{}, berr
	}

	b := Bid{AuctionID: auctionID, BidderID: bidderID, Amount: amount}
	err = tx.QueryRow(ctx, `
		INSERT INTO bids (auction_id, bidder_id, amount) VALUES ($1, $2, $3)
		RETURNING bid_id, placed_at`, auctionID, bidderID, amount).
		Scan(&b.BidID, &b.PlacedAt)
	if err != nil {
		return Bid{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE auctions SET highest_bid_id = $2, highest_bidder_id = $3, highest_amount = $4
		WHERE auction_id = $1`, auctionID, b.BidID, bidderID, amount)
	if err != nil {
		return Bid{}, err
	}

	env, err := events.New(events.SourceBidding, events.TypeBidPlaced, events.BidPlaced{
		AuctionID:        auctionID,
		BidID:            b.BidID,
		BidderID:         bidderID,
		Amount:           amount,
		PreviousBidderID: a.HighestBidderID,
		PreviousAmount:   a.HighestAmount,
	})
	if err != nil {
		return Bid{}, err
	}
	if err := insertOutbox(ctx, tx, env); err != nil {
		return Bid{}, err
	}
	return b, tx.Commit(ctx)
}

func insertOutbox(ctx context.Context, tx pgx.Tx, env events.Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox (event_id, envelope) VALUES ($1, $2)`, env.EventID, body)
	return err
}

// ApplyEvent handles an event from bidding.queue. The eventId is recorded in
// processed_events in the same transaction, so a redelivered event is
// reported as a duplicate and changes nothing.
func (s *Store) ApplyEvent(ctx context.Context, env events.Envelope) (duplicate bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`INSERT INTO processed_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, env.EventID)
	if err != nil {
		return false, fmt.Errorf("record eventId: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return true, nil
	}

	switch env.EventType {
	case events.TypeAuctionStarted:
		d, err := events.Decode[events.AuctionStarted](env)
		if err != nil {
			return false, err
		}
		if d.AuctionID == "" {
			return false, errors.New("AuctionStarted without auctionId")
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO auctions (auction_id, item_title, artisan_id, start_price, status, ends_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (auction_id) DO NOTHING`,
			d.AuctionID, d.ItemTitle, d.ArtisanID, d.StartPrice, statusOpen, d.EndsAt)
		if err != nil {
			return false, err
		}

	case events.TypeAuctionClosed:
		d, err := events.Decode[events.AuctionClosed](env)
		if err != nil {
			return false, err
		}
		if d.AuctionID == "" {
			return false, errors.New("AuctionClosed without auctionId")
		}
		// Upsert so a close is never lost, even if we somehow missed the start.
		_, err = tx.Exec(ctx, `
			INSERT INTO auctions (auction_id, item_title, artisan_id, status, winner_id, final_price, closed_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (auction_id) DO UPDATE
			SET status = EXCLUDED.status, winner_id = EXCLUDED.winner_id,
			    final_price = EXCLUDED.final_price, closed_at = EXCLUDED.closed_at`,
			d.AuctionID, d.ItemTitle, d.ArtisanID, statusClosed, d.WinnerID, d.FinalPrice)
		if err != nil {
			return false, err
		}
	}
	// Other event types aren't bound to bidding.queue; recording the id is enough.

	return false, tx.Commit(ctx)
}

func (s *Store) AuctionBids(ctx context.Context, auctionID string) (AuctionBids, error) {
	var a AuctionBids
	err := s.pool.QueryRow(ctx, `
		SELECT auction_id, item_title, status, start_price, ends_at,
		       highest_amount, highest_bidder_id, winner_id
		FROM auctions WHERE auction_id = $1`, auctionID).
		Scan(&a.AuctionID, &a.ItemTitle, &a.Status, &a.StartPrice, &a.EndsAt,
			&a.HighestAmount, &a.HighestBidderID, &a.WinnerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, errAuctionNotFound
	}
	if err != nil {
		return a, err
	}

	rows, _ := s.pool.Query(ctx, `
		SELECT bid_id, auction_id, bidder_id, amount, placed_at
		FROM bids WHERE auction_id = $1 ORDER BY amount DESC`, auctionID)
	a.Bids, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Bid])
	return a, err
}

func (s *Store) BidsByBidder(ctx context.Context, bidderID string) ([]BidderBid, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT b.bid_id, b.auction_id, b.bidder_id, b.amount, b.placed_at,
		       a.item_title, a.status, a.highest_bid_id = b.bid_id
		FROM bids b JOIN auctions a USING (auction_id)
		WHERE b.bidder_id = $1 ORDER BY b.placed_at DESC`, bidderID)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (BidderBid, error) {
		var b BidderBid
		err := r.Scan(&b.BidID, &b.AuctionID, &b.BidderID, &b.Amount, &b.PlacedAt,
			&b.ItemTitle, &b.AuctionStatus, &b.IsHighest)
		return b, err
	})
}
