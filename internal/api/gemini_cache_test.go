package api_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-llm-client/internal/api"
)

func TestCachePolicy_CalculatePolicyTTL(t *testing.T) {
	policy := api.NewCachePolicy()
	now := time.Now()

	t.Run("Empty hint returns default expiry", func(t *testing.T) {
		expiresAt, reason := policy.CalculatePolicyTTL("")

		// The zero-time bug check:
		assert.False(t, expiresAt.IsZero(), "ExpiresAt should not be a zero time")
		assert.True(t, expiresAt.After(now), "ExpiresAt should be in the future")
		assert.Contains(t, reason, "default")
	})

	t.Run("Valid future hint is accepted", func(t *testing.T) {
		// Ask for 5 hours from now
		hint := now.Add(5 * time.Hour).Format(time.RFC3339)

		expiresAt, reason := policy.CalculatePolicyTTL(hint)

		// Due to RFC3339 string rounding, we check Unix seconds
		assert.Equal(t, now.Add(5*time.Hour).Unix(), expiresAt.Unix())
		assert.Equal(t, "set by hint", reason)
	})

	t.Run("Hint exceeding max duration is capped", func(t *testing.T) {
		// Ask for 100 hours from now (Policy max is 48)
		hint := now.Add(100 * time.Hour).Format(time.RFC3339)

		expiresAt, reason := policy.CalculatePolicyTTL(hint)

		expectedMax := now.Add(48 * time.Hour)

		// Should cap exactly at max duration
		assert.WithinDuration(t, expectedMax, expiresAt, time.Second)
		assert.Equal(t, "Policy override: capping requested expiry", reason)
	})

	t.Run("Past hint falls back to default", func(t *testing.T) {
		// Ask for yesterday
		hint := now.Add(-24 * time.Hour).Format(time.RFC3339)

		expiresAt, reason := policy.CalculatePolicyTTL(hint)

		assert.True(t, expiresAt.After(now), "ExpiresAt should bounce back to future default")
		assert.Contains(t, reason, "default")
	})

	t.Run("Invalid hint string falls back to default", func(t *testing.T) {
		expiresAt, reason := policy.CalculatePolicyTTL("not-a-timestamp")

		assert.True(t, expiresAt.After(now))
		assert.Contains(t, reason, "default")
	})
}
