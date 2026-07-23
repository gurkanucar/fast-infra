package main

import (
	"testing"
	"time"
)

func TestHumanAgo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-50 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := humanAgo(c.at); got != c.want {
			t.Errorf("humanAgo(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}
