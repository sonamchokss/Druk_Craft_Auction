package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/service"
)

type api struct {
	store *Store
	relay *Relay
}

func (a *api) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auctions/{id}/bids", a.placeBid)
	mux.HandleFunc("GET /auctions/{id}/bids", a.listBids)
	mux.HandleFunc("GET /bids", a.bidsByBidder)
}

type placeBidRequest struct {
	BidderID string `json:"bidderId"`
	Amount   int64  `json:"amount"`
}

// POST /auctions/{id}/bids  {"bidderId": "U-7", "amount": 4500}
func (a *api) placeBid(w http.ResponseWriter, r *http.Request) {
	var req placeBidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		service.WriteError(w, http.StatusBadRequest, "Invalid bid — amount must be a whole number of Ngultrum")
		return
	}
	req.BidderID = strings.TrimSpace(req.BidderID)
	if req.BidderID == "" {
		service.WriteError(w, http.StatusBadRequest, "bidderId is required")
		return
	}
	if req.Amount <= 0 {
		service.WriteError(w, http.StatusBadRequest, "Bid amount must be greater than zero")
		return
	}

	bid, err := a.store.PlaceBid(r.Context(), r.PathValue("id"), req.BidderID, req.Amount)
	if !a.writeErr(w, err) {
		return
	}
	a.relay.Kick() // publish BidPlaced now rather than on the next tick
	service.WriteJSON(w, http.StatusCreated, bid)
}

// GET /auctions/{id}/bids — auction summary plus bid history, highest first.
func (a *api) listBids(w http.ResponseWriter, r *http.Request) {
	res, err := a.store.AuctionBids(r.Context(), r.PathValue("id"))
	if !a.writeErr(w, err) {
		return
	}
	service.WriteJSON(w, http.StatusOK, res)
}

// GET /bids?bidderId=U-7 — a buyer's bids across auctions, for the buyer page.
func (a *api) bidsByBidder(w http.ResponseWriter, r *http.Request) {
	bidderID := r.URL.Query().Get("bidderId")
	if bidderID == "" {
		service.WriteError(w, http.StatusBadRequest, "bidderId query parameter is required")
		return
	}
	bids, err := a.store.BidsByBidder(r.Context(), bidderID)
	if !a.writeErr(w, err) {
		return
	}
	service.WriteJSON(w, http.StatusOK, bids)
}

// writeErr writes err as a response and reports whether the caller may continue.
func (a *api) writeErr(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	var be *bidError
	if errors.As(err, &be) {
		service.WriteError(w, be.Status, be.Msg)
		return false
	}
	slog.Error("request failed", "err", err)
	service.WriteError(w, http.StatusInternalServerError, "Something went wrong — please try again")
	return false
}
