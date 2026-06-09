package config

import (
	"testing"
	"time"
)

func TestAuthExpirySecondsUseRemainingAndElapsedFields(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

	future := Auth{ExpiresAt: now.Add(2 * time.Hour)}
	if got := future.ExpiresInSeconds(now); got != 7200 {
		t.Fatalf("future ExpiresInSeconds = %d, want 7200", got)
	}
	if got := future.ExpiredAgoSeconds(now); got != 0 {
		t.Fatalf("future ExpiredAgoSeconds = %d, want 0", got)
	}

	expired := Auth{ExpiresAt: now.Add(-90 * time.Second)}
	if got := expired.ExpiresInSeconds(now); got != 0 {
		t.Fatalf("expired ExpiresInSeconds = %d, want 0", got)
	}
	if got := expired.ExpiredAgoSeconds(now); got != 90 {
		t.Fatalf("expired ExpiredAgoSeconds = %d, want 90", got)
	}
}
