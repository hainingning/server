package order

import (
	"testing"
	"time"
)

func TestIsSubscriptionExpiredForReset(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    uint8
		expireAt  time.Time
		isExpired bool
	}{
		{
			name:      "status already expired",
			status:    3,
			expireAt:  now.Add(time.Hour),
			isExpired: true,
		},
		{
			name:      "expiration passed before scheduler scan",
			status:    1,
			expireAt:  now.Add(-time.Second),
			isExpired: true,
		},
		{
			name:      "expiration is exactly now",
			status:    1,
			expireAt:  now,
			isExpired: true,
		},
		{
			name:      "active subscription",
			status:    1,
			expireAt:  now.Add(time.Second),
			isExpired: false,
		},
		{
			name:      "permanent subscription",
			status:    1,
			expireAt:  time.UnixMilli(0),
			isExpired: false,
		},
		{
			name:      "zero expiration",
			status:    1,
			expireAt:  time.Time{},
			isExpired: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSubscriptionExpiredForReset(tt.status, tt.expireAt, now); got != tt.isExpired {
				t.Fatalf("isSubscriptionExpiredForReset() = %v, want %v", got, tt.isExpired)
			}
		})
	}
}
