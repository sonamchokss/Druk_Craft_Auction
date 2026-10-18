package reliability

import (
	amqp "github.com/rabbitmq/amqp091-go"
	"testing"
)

func TestRetryCount(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int64
	}{{nil, 0}, {int32(2), 2}, {int64(3), 3}, {int(1), 1}, {int16(2), 2}, {int8(1), 1}, {int64(-1), 0}, {"3", 0}} {
		if got := RetryCount(amqp.Table{"x-retry-count": tc.value}); got != tc.want {
			t.Errorf("%v: got %d want %d", tc.value, got, tc.want)
		}
	}
}
