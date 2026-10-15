package logx

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sonamchokss/Druk_Craft_Auction/shared/events"
)

func TestEventLogsServiceOnce(t *testing.T) {
	env := events.Envelope{EventID: "e-1", EventType: events.TypeBidPlaced, Data: []byte(`{"auctionId":"A-1"}`)}

	t.Run("after Init", func(t *testing.T) {
		var buf bytes.Buffer
		initTo("bidding-service", &buf)
		Event("bidding-service", env, Published)

		if n := strings.Count(buf.String(), `"service":`); n != 1 {
			t.Fatalf(`"service" appears %d times, want 1: %s`, n, buf.String())
		}
	})

	t.Run("without Init", func(t *testing.T) {
		var buf bytes.Buffer
		initTo("", &buf) // plain logger, as if Init was never called
		Event("bidding-service", env, Published)

		if !strings.Contains(buf.String(), `"service":"bidding-service"`) {
			t.Fatalf("service missing: %s", buf.String())
		}
	})
}
