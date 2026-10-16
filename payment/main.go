package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/db"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/rabbitmq"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/reliability"
	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

const initialBalance int64 = 10000
const schema = `CREATE TABLE IF NOT EXISTS wallets(buyer_id TEXT PRIMARY KEY,balance BIGINT NOT NULL CHECK(balance>=0));
 CREATE TABLE IF NOT EXISTS invoices(auction_id TEXT PRIMARY KEY,buyer_id TEXT NOT NULL,item_title TEXT NOT NULL,
 amount BIGINT NOT NULL CHECK(amount>0),status TEXT NOT NULL,receipt_no TEXT,reason TEXT,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
 CREATE INDEX IF NOT EXISTS invoices_buyer ON invoices(buyer_id);`

type Store struct{ pool *pgxpool.Pool }

func (s *Store) Apply(ctx context.Context, env events.Envelope) (bool, error) {
	return reliability.Apply(ctx, s.pool, env, func(tx pgx.Tx) error {
		if env.EventType != events.TypeAuctionClosed {
			return errors.New("unexpected payment event")
		}
		d, err := events.Decode[events.AuctionClosed](env)
		if err != nil {
			return err
		}
		if d.AuctionID == "" {
			return errors.New("missing auctionId")
		}
		if d.WinnerID == nil {
			if d.FinalPrice != 0 {
				return errors.New("unsold auction has price")
			}
			return nil
		}
		if *d.WinnerID == "" || d.FinalPrice <= 0 {
			return errors.New("invalid winner or price")
		}
		// Auction ID additionally prevents charging twice for distinct close event IDs.
		tag, err := tx.Exec(ctx, `INSERT INTO invoices(auction_id,buyer_id,item_title,amount,status) VALUES($1,$2,$3,$4,'Pending') ON CONFLICT DO NOTHING`, d.AuctionID, *d.WinnerID, d.ItemTitle, d.FinalPrice)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if _, err = tx.Exec(ctx, `INSERT INTO wallets(buyer_id,balance) VALUES($1,$2) ON CONFLICT DO NOTHING`, *d.WinnerID, initialBalance); err != nil {
			return err
		}
		tag, err = tx.Exec(ctx, `UPDATE wallets SET balance=balance-$2 WHERE buyer_id=$1 AND balance >= $2`, *d.WinnerID, d.FinalPrice)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			reason := "insufficient balance"
			if _, err = tx.Exec(ctx, `UPDATE invoices SET status='Failed',reason=$2 WHERE auction_id=$1`, d.AuctionID, reason); err != nil {
				return err
			}
			return reliability.Enqueue(ctx, tx, events.SourcePayment, events.TypePaymentFailed, events.PaymentFailed{AuctionID: d.AuctionID, WinnerID: *d.WinnerID, Amount: d.FinalPrice, Reason: reason})
		}
		receipt := fmt.Sprintf("R-%s-%s", time.Now().UTC().Format("20060102"), uuid.NewString())
		if _, err = tx.Exec(ctx, `UPDATE invoices SET status='Completed',receipt_no=$2 WHERE auction_id=$1`, d.AuctionID, receipt); err != nil {
			return err
		}
		return reliability.Enqueue(ctx, tx, events.SourcePayment, events.TypePaymentCompleted, events.PaymentCompleted{AuctionID: d.AuctionID, WinnerID: *d.WinnerID, Amount: d.FinalPrice, ReceiptNo: receipt})
	})
}

type Invoice struct {
	AuctionID string    `json:"auctionId"`
	BuyerID   string    `json:"buyerId"`
	ItemTitle string    `json:"itemTitle"`
	Amount    int64     `json:"amount"`
	Status    string    `json:"status"`
	ReceiptNo *string   `json:"receiptNo"`
	Reason    *string   `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) Payments(w http.ResponseWriter, r *http.Request) {
	buyer := r.URL.Query().Get("buyerId")
	if buyer == "" {
		service.WriteError(w, 400, "buyerId is required")
		return
	}
	rows, err := s.pool.Query(r.Context(), `SELECT auction_id,buyer_id,item_title,amount,status,receipt_no,reason,created_at FROM invoices WHERE buyer_id=$1 ORDER BY created_at DESC`, buyer)
	if err != nil {
		service.WriteError(w, 500, "could not load payments")
		return
	}
	defer rows.Close()
	result := []Invoice{}
	for rows.Next() {
		var d Invoice
		if err = rows.Scan(&d.AuctionID, &d.BuyerID, &d.ItemTitle, &d.Amount, &d.Status, &d.ReceiptNo, &d.Reason, &d.CreatedAt); err != nil {
			service.WriteError(w, 500, "could not load payments")
			return
		}
		result = append(result, d)
	}
	if rows.Err() != nil {
		service.WriteError(w, 500, "could not load payments")
		return
	}
	service.WriteJSON(w, 200, result)
}
func (s *Store) Wallet(w http.ResponseWriter, r *http.Request) {
	buyer := r.PathValue("buyerId")
	// Visiting a demo buyer creates its wallet once; restart never resets balances.
	_, err := s.pool.Exec(r.Context(), `INSERT INTO wallets(buyer_id,balance) VALUES($1,$2) ON CONFLICT DO NOTHING`, buyer, initialBalance)
	var balance int64
	if err == nil {
		err = s.pool.QueryRow(r.Context(), `SELECT balance FROM wallets WHERE buyer_id=$1`, buyer).Scan(&balance)
	}
	if err != nil {
		service.WriteError(w, 500, "could not load wallet")
		return
	}
	service.WriteJSON(w, 200, map[string]any{"buyerId": buyer, "balance": balance})
}
func main() {
	app, ctx, cancel := service.New(events.SourcePayment)
	defer cancel()
	if _, err := app.DB.Exec(ctx, db.ProcessedEventsDDL+reliability.OutboxDDL+schema); err != nil {
		slog.Error("schema failed", "err", err)
		os.Exit(1)
	}
	store := &Store{app.DB}
	app.Mux.HandleFunc("GET /payments", store.Payments)
	app.Mux.HandleFunc("GET /wallets/{buyerId}", store.Wallet)
	var wg sync.WaitGroup
	failed := make(chan error, 2)
	start := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				failed <- err
				cancel()
			}
		}()
	}
	start(func() error {
		return reliability.Consume(ctx, app.Conn, app.Channel, rabbitmq.PaymentQueue, events.SourcePayment, store.Apply)
	})
	start(func() error { return reliability.Relay(ctx, app.DB, app.Publisher) })
	app.Run(ctx)
	wg.Wait()
	select {
	case err := <-failed:
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	default:
	}
}
