package session

import (
	"context"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// Service defines the business logic operations for workspace sessions.
type Service interface {
	GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error)

	// Ephemeral Queue Operations
	CreateProposal(ctx context.Context, proposal *builder.ChangeProposal) error
	ListProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error)
	RemoveProposal(ctx context.Context, sessionID urn.URN, proposalID string) error

	// Passed through for convenience so the API only needs one dependency
	SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error
}

type statelessService struct {
	store store.SessionStore
}

// NewService creates a new stateless session domain service.
func NewService(store store.SessionStore) Service {
	return &statelessService{
		store: store,
	}
}

func (s *statelessService) SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error {
	return s.store.SaveCompiledCache(ctx, firestoreCacheID, cache)
}

func (s *statelessService) GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error) {
	return s.store.GetSession(ctx, sessionID)
}

func (s *statelessService) CreateProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	return s.store.SaveProposal(ctx, proposal)
}

func (s *statelessService) ListProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error) {
	return s.store.GetProposalsBySession(ctx, sessionID)
}

// Note: proposalID remains a string here because it is an ephemeral ID generated
// temporarily for the queue, as discussed.
func (s *statelessService) RemoveProposal(ctx context.Context, sessionID urn.URN, proposalID string) error {
	return s.store.DeleteProposal(ctx, sessionID, proposalID)
}
