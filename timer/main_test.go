package main

import (
	"testing"
	"time"
)

func TestExtendedDeadline(t *testing.T) {
	end := time.Date(2026, 10, 20, 10, 20, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		offset time.Duration
		extend bool
	}{
		{"outside window", -31 * time.Second, false}, {"window boundary", -30 * time.Second, true}, {"last second", -time.Second, true}, {"at deadline", 0, false}, {"late", time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := end
			if tc.extend {
				want = end.Add(30 * time.Second)
			}
			if got := extendedDeadline(end, end.Add(tc.offset)); !got.Equal(want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
}
