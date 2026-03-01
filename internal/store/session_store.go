package store

import (
	"context"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// SessionStore defines the contract for persisting LLM chat sessions,
// external compiled cache metadata, and the ephemeral proposal queue.
type SessionStore interface {
	// Compiled Caches (Metadata pointers)
	SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error
	ListCompiledCaches(ctx context.Context, firestoreCacheID string) ([]builder.CompiledCache, error)

	// Sessions
	GetSession(ctx context.Context, sessionID string) (*builder.Session, error)
	SaveSession(ctx context.Context, session *builder.Session) error

	// Ephemeral Proposal Queue
	SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error
	GetProposalsBySession(ctx context.Context, sessionID string) ([]builder.ChangeProposal, error)
	DeleteProposal(ctx context.Context, sessionID, proposalID string) error
}
