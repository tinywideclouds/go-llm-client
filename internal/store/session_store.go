// session_store.go
package store

import (
	"context"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

type SessionStore interface {
	SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error
	ListCompiledCaches(ctx context.Context, firestoreCacheID urn.URN) ([]builder.CompiledCache, error)

	GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error)
	SaveSession(ctx context.Context, session *builder.Session) error

	SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error
	GetProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error)
	DeleteProposal(ctx context.Context, sessionID urn.URN, proposalID string) error
}
