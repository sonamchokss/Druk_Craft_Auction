// Package events is the Go form of docs/events.md — the event contract every
// service shares. Change this file only together with docs/events.md.
package events

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event type names (the envelope's eventType).
const (
	TypeAuctionStarted   = "AuctionStarted"
	TypeBidPlaced        = "BidPlaced"
	TypeAuctionExtended  = "AuctionExtended"
	TypeAuctionClosed    = "AuctionClosed"
	TypePaymentCompleted = "PaymentCompleted"
	TypePaymentFailed    = "PaymentFailed"
)

// Routing keys on the auction.events topic exchange.
const (
	KeyAuctionStarted   = "auction.started"
	KeyBidPlaced        = "bid.placed"
	KeyAuctionExtended  = "auction.extended"
	KeyAuctionClosed    = "auction.closed"
	KeyPaymentCompleted = "payment.completed"
	KeyPaymentFailed    = "payment.failed"
)

// Source names used in the envelope.
const (
	SourceListing      = "listing-service"
	SourceBidding      = "bidding-service"
	SourceTimer        = "timer-service"
	SourcePayment      = "payment-service"
	SourceNotification = "notification-service"
)

// RoutingKeys maps each event type to its routing key.
var RoutingKeys = map[string]string{
	TypeAuctionStarted:   KeyAuctionStarted,
	TypeBidPlaced:        KeyBidPlaced,
	TypeAuctionExtended:  KeyAuctionExtended,
	TypeAuctionClosed:    KeyAuctionClosed,
	TypePaymentCompleted: KeyPaymentCompleted,
	TypePaymentFailed:    KeyPaymentFailed,
}

// Envelope wraps every event on the wire.
type Envelope struct {
	EventID    string          `json:"eventId"`
	EventType  string          `json:"eventType"`
	OccurredAt time.Time       `json:"occurredAt"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

// AuctionStarted — published by Listing.
type AuctionStarted struct {
	AuctionID  string    `json:"auctionId"`
	ItemID     string    `json:"itemId"`
	ItemTitle  string    `json:"itemTitle"`
	ArtisanID  string    `json:"artisanId"`
	StartPrice int64     `json:"startPrice"`
	EndsAt     time.Time `json:"endsAt"`
}

// BidPlaced — published by Bidding. Previous* fields are nil on the first bid.
type BidPlaced struct {
	AuctionID        string  `json:"auctionId"`
	BidID            string  `json:"bidId"`
	BidderID         string  `json:"bidderId"`
	Amount           int64   `json:"amount"`
	PreviousBidderID *string `json:"previousBidderId"`
	PreviousAmount   *int64  `json:"previousAmount"`
}

// AuctionExtended — published by Timer (anti-sniping).
type AuctionExtended struct {
	AuctionID string    `json:"auctionId"`
	NewEndsAt time.Time `json:"newEndsAt"`
}

// AuctionClosed — published by Timer. WinnerID is nil and FinalPrice is 0 when there were no bids.
type AuctionClosed struct {
	AuctionID  string  `json:"auctionId"`
	ItemTitle  string  `json:"itemTitle"`
	ArtisanID  string  `json:"artisanId"`
	WinnerID   *string `json:"winnerId"`
	FinalPrice int64   `json:"finalPrice"`
}

// PaymentCompleted — published by Payment.
type PaymentCompleted struct {
	AuctionID string `json:"auctionId"`
	WinnerID  string `json:"winnerId"`
	Amount    int64  `json:"amount"`
	ReceiptNo string `json:"receiptNo"`
}

// PaymentFailed — published by Payment.
type PaymentFailed struct {
	AuctionID string `json:"auctionId"`
	WinnerID  string `json:"winnerId"`
	Amount    int64  `json:"amount"`
	Reason    string `json:"reason"`
}

// New builds an envelope with a fresh eventId and the current UTC time.
func New(source, eventType string, data any) (Envelope, error) {
	if _, ok := RoutingKeys[eventType]; !ok {
		return Envelope{}, fmt.Errorf("unknown event type %q", eventType)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s data: %w", eventType, err)
	}
	return Envelope{
		EventID:    uuid.NewString(),
		EventType:  eventType,
		OccurredAt: time.Now().UTC().Truncate(time.Second),
		Source:     source,
		Data:       raw,
	}, nil
}

// Parse decodes a message body into an envelope and checks the required fields.
// A malformed body returns an error — treat that as a poison message.
func Parse(body []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, fmt.Errorf("invalid envelope: %w", err)
	}
	if env.EventID == "" || env.EventType == "" || len(env.Data) == 0 {
		return Envelope{}, fmt.Errorf("invalid envelope: missing eventId, eventType or data")
	}
	return env, nil
}

// Decode unmarshals the envelope's data into the payload type T.
//
//	closed, err := events.Decode[events.AuctionClosed](env)
func Decode[T any](env Envelope) (T, error) {
	var v T
	if err := json.Unmarshal(env.Data, &v); err != nil {
		return v, fmt.Errorf("decode %s data: %w", env.EventType, err)
	}
	return v, nil
}

// AuctionID pulls auctionId out of any payload, for logging.
func (e Envelope) AuctionID() string {
	var v struct {
		AuctionID string `json:"auctionId"`
	}
	_ = json.Unmarshal(e.Data, &v)
	return v.AuctionID
}
