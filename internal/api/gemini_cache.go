package api

import (
	"time"
)

type CachePolicy struct {
	maxDuration time.Duration
	preferedEnd time.Duration
	extention   time.Duration
}

func NewCachePolicy() *CachePolicy {
	maxDuration := time.Hour * 48
	preferedEnd := time.Hour * 18 // i.e six in the evening

	return &CachePolicy{maxDuration: maxDuration, preferedEnd: preferedEnd, extention: time.Hour}
}

func (a *CachePolicy) CalculatePolicyTTL(hint string) (time.Time, string) {
	now := time.Now()
	// Policy: Max expiry is 48 hours from now
	maxExpiry := now.Add(a.maxDuration)

	var expiresAt time.Time
	reason := ""

	// apply our default policy
	if hint != "" {
		hintTime, err := time.Parse(time.RFC3339, hint)
		if err == nil && hintTime.After(now) {
			expiresAt = hintTime
			reason = "set by hint"
		} else {
			expiresAt, reason = a.defaultExpiry()
		}
	} else {
		// FIX: Safely fallback to default if hint is empty string
		expiresAt, reason = a.defaultExpiry()
	}

	// Apply the Hard Ceiling: Microservice policy override
	if expiresAt.After(maxExpiry) {
		reason = "Policy override: capping requested expiry"
		expiresAt = maxExpiry
	}

	return expiresAt, reason
}

func (a *CachePolicy) defaultExpiry() (time.Time, string) {
	now := time.Now()
	reason := "default expiry"
	preferedExpiry := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(a.preferedEnd)

	// If it's already past 6:00 PM, give them an extra hour extension
	if now.After(preferedExpiry) {
		preferedExpiry = now.Add(a.extention)
		reason = "default short expiry"
	}
	return preferedExpiry, reason
}
