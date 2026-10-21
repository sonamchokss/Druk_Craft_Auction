package store

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonamchokss/druk-craft-auction/notification/internal/model"
)

func TestFormatMoney(t *testing.T) {
	for _, test := range []struct {
		in   int64
		want string
	}{{4500, "Nu. 4,500"}, {0, "Nu. 0"}, {-1200, "Nu. -1,200"}} {
		if got := FormatMoney(test.in); got != test.want {
			t.Errorf("%d: got %q want %q", test.in, got, test.want)
		}
	}
}

func TestProcessDuplicateSkipsNotifications(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO processed_events(event_id) VALUES($1) ON CONFLICT DO NOTHING RETURNING event_id`)).
		WithArgs("event-1").WillReturnRows(sqlmock.NewRows([]string{"event_id"}))
	mock.ExpectRollback()

	err = New(db).Process(context.Background(), "event-1", []model.Notification{{
		UserID: "U-7", Type: "outbid", Message: "ignored", AuctionID: "a1", EventID: "event-1",
	}}, nil, "a1")
	if err != ErrDuplicate {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
