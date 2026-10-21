package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
)

var ErrDuplicate = errors.New("duplicate event")

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS notifications (
 id SERIAL PRIMARY KEY, user_id TEXT NOT NULL, type TEXT NOT NULL, message TEXT NOT NULL,
 auction_id TEXT NOT NULL DEFAULT '', event_id TEXT NOT NULL, read BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS notifications_user_created_idx ON notifications(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS processed_events (event_id TEXT PRIMARY KEY, processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE IF NOT EXISTS auction_bidders (auction_id TEXT NOT NULL, bidder_id TEXT NOT NULL,
 PRIMARY KEY (auction_id, bidder_id));`)
	return err
}

func (s *Store) Process(ctx context.Context, eventID string, notifications []model.Notification, bidders []string, auctionID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inserted string
	err = tx.QueryRowContext(ctx, `INSERT INTO processed_events(event_id) VALUES($1) ON CONFLICT DO NOTHING RETURNING event_id`, eventID).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	for _, bidder := range bidders {
		if bidder == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO auction_bidders(auction_id,bidder_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, auctionID, bidder); err != nil {
			return err
		}
	}
	for _, notification := range notifications {
		_, err = tx.ExecContext(ctx, `INSERT INTO notifications(user_id,type,message,auction_id,event_id,read) VALUES($1,$2,$3,$4,$5,FALSE)`,
			notification.UserID, notification.Type, notification.Message, notification.AuctionID, notification.EventID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Bidders(ctx context.Context, auctionID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT bidder_id FROM auction_bidders WHERE auction_id=$1`, auctionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var bidder string
		if err := rows.Scan(&bidder); err != nil {
			return nil, err
		}
		result = append(result, bidder)
	}
	return result, rows.Err()
}

func (s *Store) List(ctx context.Context, userID string, unread bool) ([]model.Notification, error) {
	query := `SELECT id,user_id,type,message,auction_id,event_id,read,created_at FROM notifications WHERE user_id=$1`
	args := []any{userID}
	if unread {
		query += ` AND read=FALSE`
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Message, &n.AuctionID, &n.EventID, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func (s *Store) MarkRead(ctx context.Context, id string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read=TRUE WHERE id=$1`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return fmt.Errorf("notification %s not found", id)
	}
	return err
}

func FormatMoney(amount int64) string {
	sign := ""
	value := amount
	if value < 0 {
		sign = "-"
		value = -value
	}
	raw := fmt.Sprintf("%d", value)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return "Nu. " + sign + raw
}

func ParseTime(value string) time.Time { parsed, _ := time.Parse(time.RFC3339, value); return parsed }
func Join(parts ...string) string      { return strings.TrimSpace(strings.Join(parts, " ")) }
