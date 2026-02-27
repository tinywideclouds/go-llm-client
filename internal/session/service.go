package session

import (
	"context"
	"fmt"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// Service defines the business logic operations for workspace sessions.
type Service interface {
	GetSession(ctx context.Context, sessionID string) (*builder.Session, error)
	AcceptProposal(ctx context.Context, sessionID, proposalID string) error
	RejectProposal(ctx context.Context, sessionID, proposalID string) error

	// Passed through for convenience so the API only needs one dependency
	SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error
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

func (s *statelessService) SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
	return s.store.SaveCompiledCache(ctx, firestoreCacheID, cache)
}

func (s *statelessService) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	return s.store.GetSession(ctx, sessionID)
}

func (s *statelessService) AcceptProposal(ctx context.Context, sessionID, proposalID string) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to retrieve session: %w", err)
	}

	prop, exists := sess.PendingProposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal not found")
	}
	if prop.Status != builder.StatusPending {
		return fmt.Errorf("proposal is no longer pending")
	}

	// Apply the business logic
	prop.Status = builder.StatusAccepted
	sess.PendingProposals[proposalID] = prop
	sess.AcceptedOverlays[prop.FilePath] = builder.FileState{
		Content:   prop.NewContent,
		IsDeleted: false,
	}

	if err := s.store.SaveSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to save session state: %w", err)
	}

	return nil
}

func (s *statelessService) RejectProposal(ctx context.Context, sessionID, proposalID string) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to retrieve session: %w", err)
	}

	prop, exists := sess.PendingProposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal not found")
	}
	if prop.Status != builder.StatusPending {
		return fmt.Errorf("proposal is no longer pending")
	}

	// Apply the business logic
	prop.Status = builder.StatusRejected
	sess.PendingProposals[proposalID] = prop

	if err := s.store.SaveSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to save session state: %w", err)
	}

	return nil
}
