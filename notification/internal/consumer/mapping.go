package consumer

import (
	"encoding/json"
	"fmt"

	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/store"
)

type Cache struct{ Items map[string]Item }
type Item struct{ Title, ArtisanID string }

func NewCache() *Cache { return &Cache{Items: map[string]Item{}} }

func Map(cache *Cache, envelope model.Envelope, bidders []string) ([]model.Notification, []string, error) {
	if envelope.EventID == "" {
		return nil, nil, fmt.Errorf("missing eventId")
	}
	var data model.EventData
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, nil, err
	}
	item := cache.Items[data.AuctionID]
	title := data.ItemTitle
	if title == "" {
		title = item.Title
	}
	artisan := data.ArtisanID
	if artisan == "" {
		artisan = item.ArtisanID
	}
	n := func(user, typ, message string) model.Notification {
		return model.Notification{UserID: user, Type: typ, Message: message, AuctionID: data.AuctionID, EventID: envelope.EventID}
	}
	switch envelope.EventType {
	case "AuctionStarted":
		cache.Items[data.AuctionID] = Item{Title: title, ArtisanID: artisan}
		return []model.Notification{n(artisan, "started", fmt.Sprintf("Auction started: %s", title))}, nil, nil
	case "BidPlaced":
		if data.BidderID != "" {
			bidders = append(bidders, data.BidderID)
		}
		if data.PreviousBidderID == "" {
			return nil, bidders, nil
		}
		return []model.Notification{n(data.PreviousBidderID, "outbid", fmt.Sprintf("You've been outbid on %s! New bid: %s", title, store.FormatMoney(data.Amount)))}, bidders, nil
	case "AuctionExtended":
		result := make([]model.Notification, 0, len(bidders))
		for _, bidder := range bidders {
			result = append(result, n(bidder, "extended", "Auction extended by 30s"))
		}
		return result, nil, nil
	case "AuctionClosed":
		cache.Items[data.AuctionID] = Item{Title: title, ArtisanID: artisan}
		result := []model.Notification{}
		if data.WinnerID != nil && *data.WinnerID != "" {
			result = append(result, n(*data.WinnerID, "won", fmt.Sprintf("You won %s for %s", title, store.FormatMoney(data.FinalPrice))))
		}
		if artisan != "" {
			typ := "closed"
			message := fmt.Sprintf("Auction closed: %s", title)
			if data.WinnerID == nil || *data.WinnerID == "" {
				typ = "unsold"
				message = fmt.Sprintf("Auction ended unsold: %s", title)
			}
			result = append(result, n(artisan, typ, message))
		}
		return result, nil, nil
	case "PaymentCompleted":
		winnerID := ""
		if data.WinnerID != nil {
			winnerID = *data.WinnerID
		}
		result := []model.Notification{n(winnerID, "receipt", fmt.Sprintf("Payment received. Receipt %s for %s", data.ReceiptNo, store.FormatMoney(data.Amount)))}
		if artisan != "" {
			result = append(result, n(artisan, "sold", fmt.Sprintf("Your item sold for %s", store.FormatMoney(data.Amount))))
		}
		return result, nil, nil
	case "PaymentFailed":
		winnerID := ""
		if data.WinnerID != nil {
			winnerID = *data.WinnerID
		}
		return []model.Notification{n(winnerID, "payment_failed", fmt.Sprintf("Payment failed: %s", data.Reason))}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported event type %q", envelope.EventType)
	}
}
