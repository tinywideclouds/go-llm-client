package tools

import (
	"encoding/base64"
	"fmt"

	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

const (
	DomainLLM         = "llm"
	TypeCompiledCache = "compiled-cache"
)

// NewCompiledCacheURN creates a standardized URN for a compiled cache to prevent magic string drift.
func NewCompiledCacheURN(id string) (urn.URN, error) {
	return urn.New(DomainLLM, TypeCompiledCache, id)
}

// EncodeFirestoreID protects Firestore from forbidden characters (like slashes) in URN entity strings.
func EncodeFirestoreID(u urn.URN) string {
	return base64.RawURLEncoding.EncodeToString([]byte(u.EntityID()))
}

// DecodeFirestoreCompiledCacheID restores the original Compiled Cache URN from the sanitized Firestore Document ID.
func DecodeFirestoreCompiledCacheID(encoded string) (urn.URN, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return urn.URN{}, fmt.Errorf("failed to decode firestore ID: %w", err)
	}

	return NewCompiledCacheURN(string(decoded))
}
