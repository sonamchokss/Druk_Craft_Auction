package main

import (
	"net/http"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestValidateBid(t *testing.T) {
	open := auctionState{Status: statusOpen, ArtisanID: "U-1", StartPrice: 3000}
	withBid := auctionState{Status: statusOpen, StartPrice: 3000, HighestAmount: ptr[int64](4200), HighestBidderID: ptr("U-3")}

	tests := []struct {
		name       string
		auction    auctionState
		bidder     string
		amount     int64
		wantStatus int // 0 = accepted
	}{
		{"first bid at start price", open, "U-7", 3000, 0},
		{"first bid above start price", open, "U-7", 3500, 0},
		{"first bid below start price", open, "U-7", 2999, http.StatusUnprocessableEntity},
		{"higher than current highest", withBid, "U-7", 4500, 0},
		{"equal to current highest", withBid, "U-7", 4200, http.StatusUnprocessableEntity},
		{"lower than current highest", withBid, "U-7", 4000, http.StatusUnprocessableEntity},
		{"highest bidder bids again", withBid, "U-3", 5000, http.StatusConflict},
		{"artisan bids on own item", open, "U-1", 5000, http.StatusForbidden},
		{"closed auction", auctionState{Status: statusClosed, StartPrice: 3000}, "U-7", 9999, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBid(tt.auction, tt.bidder, tt.amount)
			got := 0
			if err != nil {
				got = err.Status
			}
			if got != tt.wantStatus {
				t.Fatalf("status = %d, want %d (err: %v)", got, tt.wantStatus, err)
			}
		})
	}
}
