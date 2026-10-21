package model

import (
	"encoding/json"
	"time"
)

type Envelope struct {
	EventID    string          `json:"eventId"`
	EventType  string          `json:"eventType"`
	OccurredAt time.Time       `json:"occurredAt"`
	Source     string          `json:"source"`
	Data       json.RawMessage `json:"data"`
}

type EventData struct {
	AuctionID        string  `json:"auctionId"`
	ItemID           string  `json:"itemId"`
	ItemTitle        string  `json:"itemTitle"`
	ArtisanID        string  `json:"artisanId"`
	StartPrice       int64   `json:"startPrice"`
	EndsAt           string  `json:"endsAt"`
	BidID            string  `json:"bidId"`
	BidderID         string  `json:"bidderId"`
	Amount           int64   `json:"amount"`
	PreviousBidderID string  `json:"previousBidderId"`
	PreviousAmount   int64   `json:"previousAmount"`
	WinnerID         *string `json:"winnerId"`
	FinalPrice       int64   `json:"finalPrice"`
	ReceiptNo        string  `json:"receiptNo"`
	Reason           string  `json:"reason"`
	NewEndsAt        string  `json:"newEndsAt"`
}

type Notification struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"userId"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	AuctionID string    `json:"auctionId"`
	EventID   string    `json:"eventId"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}
