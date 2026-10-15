package order

import "testing"

// Unit test of the state machine: no database needed.
func TestStatusCanBecome(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusPending, StatusPaid, true},
		{StatusPending, StatusCancelled, true},
		{StatusPending, StatusShipped, false}, // cannot ship before payment
		{StatusPaid, StatusShipped, true},
		{StatusPaid, StatusCancelled, true},
		{StatusShipped, StatusCompleted, true},
		{StatusShipped, StatusCancelled, false}, // already handed to the courier
		{StatusCompleted, StatusCancelled, false},
		{StatusCancelled, StatusPaid, false}, // final state
		{StatusCancelled, StatusCancelled, false},
	}
	for _, c := range cases {
		if got := c.from.CanBecome(c.to); got != c.want {
			t.Errorf("%s -> %s: got %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
