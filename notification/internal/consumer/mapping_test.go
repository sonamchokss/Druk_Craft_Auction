package consumer

import (
	"encoding/json"
	"testing"

	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
)

func TestBidPlacedMapsOutbid(t *testing.T) {
	cache := NewCache()
	data, _ := json.Marshal(map[string]any{"auctionId": "a1", "itemTitle": "Dragon Mask", "amount": 4500, "previousBidderId": "U-7", "bidderId": "U-3"})
	notes, bidders, err := Map(cache, model.Envelope{EventID: "e1", EventType: "BidPlaced", Data: data}, nil)
	if err != nil || len(notes) != 1 || notes[0].Type != "outbid" || notes[0].UserID != "U-7" || len(bidders) != 1 {
		t.Fatalf("unexpected mapping: %#v %#v %v", notes, bidders, err)
	}
}
func TestAuctionExtendedMapsKnownBidders(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"auctionId": "a1"})
	notes, _, err := Map(NewCache(), model.Envelope{EventID: "e2", EventType: "AuctionExtended", Data: data}, []string{"U-3", "U-7"})
	if err != nil || len(notes) != 2 || notes[0].Type != "extended" {
		t.Fatalf("unexpected mapping: %#v %v", notes, err)
	}
}
