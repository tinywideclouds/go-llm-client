package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

const (
	LlmSessionsCollection = "LlmSessions"
	CompiledCachesSubCol  = "CompiledCaches"
	ProposalsSubCol       = "Proposals" // Now an ephemeral queue under LlmSessions
)

type FirestoreSessionStore struct {
	client    *firestore.Client
	bundleCol string
	logger    *slog.Logger
}

func NewFirestoreSessionStore(client *firestore.Client, bundleCol string, logger *slog.Logger) *FirestoreSessionStore {
	return &FirestoreSessionStore{
		client:    client,
		bundleCol: bundleCol,
		logger:    logger,
	}
}

// --- Compiled Caches ---

func (s *FirestoreSessionStore) SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
	ref := s.client.Collection(s.bundleCol).Doc(firestoreCacheID).Collection(CompiledCachesSubCol).Doc(cache.ID)
	_, err := ref.Set(ctx, cache)
	if err != nil {
		return fmt.Errorf("failed to save compiled cache: %w", err)
	}
	return nil
}

func (s *FirestoreSessionStore) ListCompiledCaches(ctx context.Context, firestoreCacheID string) ([]builder.CompiledCache, error) {
	var caches []builder.CompiledCache
	iter := s.client.Collection(s.bundleCol).Doc(firestoreCacheID).Collection(CompiledCachesSubCol).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate compiled caches: %w", err)
		}

		var c builder.CompiledCache
		if err := doc.DataTo(&c); err != nil {
			s.logger.Warn("Failed to unmarshal compiled cache", "docID", doc.Ref.ID, "error", err)
			continue
		}
		c.ID = doc.Ref.ID
		caches = append(caches, c)
	}
	return caches, nil
}

// --- Sessions ---

func (s *FirestoreSessionStore) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	doc, err := s.client.Collection(LlmSessionsCollection).Doc(sessionID).Get(ctx)

	if status.Code(err) == codes.NotFound {
		s.logger.Debug("Session not found, creating new instance", "session_id", sessionID)
		return &builder.Session{
			ID:        sessionID,
			UpdatedAt: time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var sess builder.Session
	if err := doc.DataTo(&sess); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}
	sess.ID = doc.Ref.ID

	return &sess, nil
}

func (s *FirestoreSessionStore) SaveSession(ctx context.Context, session *builder.Session) error {
	session.UpdatedAt = time.Now()
	ref := s.client.Collection(LlmSessionsCollection).Doc(session.ID)

	_, err := ref.Set(ctx, session)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

// --- Ephemeral Proposal Queue ---

func (s *FirestoreSessionStore) SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	// Saved directly under the specific session
	ref := s.client.Collection(LlmSessionsCollection).Doc(proposal.SessionID).Collection(ProposalsSubCol).Doc(proposal.ID)
	_, err := ref.Set(ctx, proposal)
	if err != nil {
		return fmt.Errorf("failed to save proposal: %w", err)
	}
	return nil
}

func (s *FirestoreSessionStore) GetProposalsBySession(ctx context.Context, sessionID string) ([]builder.ChangeProposal, error) {
	var proposals []builder.ChangeProposal
	// Simple subcollection read
	iter := s.client.Collection(LlmSessionsCollection).Doc(sessionID).Collection(ProposalsSubCol).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list proposals by session: %w", err)
		}

		var p builder.ChangeProposal
		if err := doc.DataTo(&p); err != nil {
			continue
		}
		p.ID = doc.Ref.ID
		proposals = append(proposals, p)
	}
	return proposals, nil
}

func (s *FirestoreSessionStore) DeleteProposal(ctx context.Context, sessionID, proposalID string) error {
	ref := s.client.Collection(LlmSessionsCollection).Doc(sessionID).Collection(ProposalsSubCol).Doc(proposalID)
	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete proposal: %w", err)
	}
	return nil
}
