package main

import (
	"fmt"
	"net/http"
)

const (
	statusOpen   = "open"
	statusClosed = "closed"
)

// auctionState is the part of an auction a bid is validated against.
type auctionState struct {
	Status          string
	ArtisanID       string
	StartPrice      int64
	HighestAmount   *int64
	HighestBidderID *string
}

// bidError is a rejected bid: an HTTP status plus a message the UI shows as-is.
type bidError struct {
	Status int
	Msg    string
}

func (e *bidError) Error() string { return e.Msg }

// validateBid applies the bidding rules. It returns nil when the bid is accepted.
func validateBid(a auctionState, bidderID string, amount int64) *bidError {
	if a.Status != statusOpen {
		return &bidError{http.StatusConflict, "Auction is closed — bids are no longer accepted"}
	}
	// Stops shill bidding: the seller pushing up the price of their own item.
	if a.ArtisanID != "" && bidderID == a.ArtisanID {
		return &bidError{http.StatusForbidden, "You can't bid on your own item"}
	}
	if amount < a.StartPrice {
		return &bidError{http.StatusUnprocessableEntity,
			fmt.Sprintf("Bid too low — the starting price is Nu. %d", a.StartPrice)}
	}
	if a.HighestAmount != nil {
		if a.HighestBidderID != nil && *a.HighestBidderID == bidderID {
			return &bidError{http.StatusConflict, "You are already the highest bidder"}
		}
		if amount <= *a.HighestAmount {
			return &bidError{http.StatusUnprocessableEntity,
				fmt.Sprintf("Bid too low — the current highest bid is Nu. %d", *a.HighestAmount)}
		}
	}
	return nil
}
