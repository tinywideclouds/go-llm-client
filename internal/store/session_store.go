package store

import (
	"context"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// SessionStore defines the contract for persisting LLM chat sessions
// and external compiled cache metadata.
type SessionStore interface {
	// Compiled Caches (Stored under /CacheBundles/{CacheID}/CompiledCaches/)
	SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error
	ListCompiledCaches(ctx context.Context, firestoreCacheID string) ([]builder.CompiledCache, error)

	// Sessions (Stored under /LlmSessions/)

	// GetSession retrieves a session by ID. If the session does not exist,
	// it should return a cleanly initialized empty Session (acting as GetOrCreate).
	GetSession(ctx context.Context, sessionID string) (*builder.Session, error)
	SaveSession(ctx context.Context, session *builder.Session) error
}
