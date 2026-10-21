package main

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	amqp "github.com/rabbitmq/amqp091-go"
)

// withMockDB swaps the package-level db for a sqlmock and checks every
// expectation was met when the test ends.
func withMockDB(t *testing.T) sqlmock.Sqlmock {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	old := db
	db = mockDB
	t.Cleanup(func() {
		db = old
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		mockDB.Close()
	})
	return mock
}

func envelope(eventType, data string) Envelope {
	return Envelope{EventID: "6f1c2a9e-0b7d-4c1e-9a53-2d7f3e8b1c44", EventType: eventType, Data: json.RawMessage(data)}
}

func toDriverArgs(args []any) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a
	}
	return out
}

const insertProcessed = `INSERT INTO processed_events(event_id) VALUES ($1) ON CONFLICT DO NOTHING`

func TestHandleEventStatusChanges(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		data      string
		update    string
		args      []any
	}{
		{"closed with winner -> Awaiting Payment", "AuctionClosed",
			`{"auctionId":"A-1","winnerId":"U-7","finalPrice":4500}`,
			`UPDATE items SET status='Awaiting Payment'`, []any{"U-7", 4500, "A-1"}},
		{"closed without bids -> Unsold", "AuctionClosed",
			`{"auctionId":"A-1","winnerId":null,"finalPrice":0}`,
			`UPDATE items SET status='Unsold'`, []any{"A-1"}},
		{"payment completed -> Sold", "PaymentCompleted",
			`{"auctionId":"A-1","winnerId":"U-7","amount":4500,"receiptNo":"R-1"}`,
			`UPDATE items SET status='Sold', receipt_no=$1`, []any{"R-1", "A-1"}},
		{"payment failed -> Payment Failed", "PaymentFailed",
			`{"auctionId":"A-1","winnerId":"U-7","amount":4500,"reason":"insufficient balance"}`,
			`UPDATE items SET status='Payment Failed'`, []any{"A-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := withMockDB(t)
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta(insertProcessed)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(regexp.QuoteMeta(tt.update)).WithArgs(toDriverArgs(tt.args)...).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			result, err := handleEvent(envelope(tt.eventType, tt.data))
			if err != nil || result != "processed" {
				t.Fatalf("got (%q, %v), want (processed, nil)", result, err)
			}
		})
	}
}

// The eventId insert and the status change share one transaction, so a
// redelivered event changes nothing (test T8).
func TestHandleEventDuplicateIsSkipped(t *testing.T) {
	mock := withMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(insertProcessed)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	result, err := handleEvent(envelope("PaymentCompleted", `{"auctionId":"A-1","receiptNo":"R-1"}`))
	if err != nil || result != "duplicate-skipped" {
		t.Fatalf("got (%q, %v), want (duplicate-skipped, nil)", result, err)
	}
}

// Errors are returned (not swallowed) so handleDelivery retries and then dead-letters.
func TestHandleEventErrors(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(insertProcessed)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectRollback()
		if _, err := handleEvent(envelope("AuctionClosed", `{"winnerId":"U-7"}`)); err == nil {
			t.Fatal("want error for AuctionClosed without auctionId")
		}
	})
	t.Run("unknown auction", func(t *testing.T) {
		mock := withMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta(insertProcessed)).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(regexp.QuoteMeta(`UPDATE items SET status='Payment Failed'`)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectRollback()
		if _, err := handleEvent(envelope("PaymentFailed", `{"auctionId":"A-404"}`)); err == nil {
			t.Fatal("want error when no listing has this auctionId")
		}
	})
}

// Listing's queue only receives events it binds to, but anything else is
// acknowledged without touching the database.
func TestHandleEventIgnoresOtherTypes(t *testing.T) {
	withMockDB(t) // no expectations: any DB call fails the test
	for _, eventType := range []string{"AuctionStarted", "BidPlaced", "AuctionExtended"} {
		if result, err := handleEvent(envelope(eventType, `{}`)); err != nil || result != "ignored" {
			t.Errorf("%s: got (%q, %v), want (ignored, nil)", eventType, result, err)
		}
	}
}

// Invalid requests are rejected with a clear message before reaching the database.
func TestCreateListingValidation(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`not json`, "invalid JSON body"},
		{`{"artisanId":"A-1","startPrice":3000,"durationSeconds":60}`, "title is required"},
		{`{"title":"Mask","startPrice":3000,"durationSeconds":60}`, "artisanId is required"},
		{`{"title":"Mask","artisanId":"A-1","startPrice":0,"durationSeconds":60}`, "startPrice must be a positive whole number (Nu.)"},
		{`{"title":"Mask","artisanId":"A-1","startPrice":3000,"durationSeconds":0}`, "durationSeconds must be greater than 0"},
	}
	withMockDB(t) // no expectations: validation must not reach the database
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		createListing(rec, httptest.NewRequest(http.MethodPost, "/listings", strings.NewReader(tt.body)))
		var got map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusBadRequest || got["error"] != tt.want {
			t.Errorf("body %s: got %d %q, want 400 %q", tt.body, rec.Code, got["error"], tt.want)
		}
	}
}

func TestRetryCount(t *testing.T) {
	for _, tt := range []struct {
		headers amqp.Table
		want    int
	}{
		{nil, 0},
		{amqp.Table{retryHeader: int32(2)}, 2},
		{amqp.Table{retryHeader: int64(3)}, 3},
	} {
		if got := retryCount(tt.headers); got != tt.want {
			t.Errorf("retryCount(%v) = %d, want %d", tt.headers, got, tt.want)
		}
	}
}
