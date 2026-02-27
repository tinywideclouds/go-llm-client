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
)

type FirestoreSessionStore struct {
	client    *firestore.Client
	bundleCol string // Allows pointing to "CacheBundles" or "CacheBundles_Dev"
	logger    *slog.Logger
}

// NewFirestoreSessionStore initializes the persistent session store.
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
		c.ID = doc.Ref.ID // Inject the Firestore document ID back into the struct
		caches = append(caches, c)
	}

	return caches, nil
}

// --- Sessions ---

func (s *FirestoreSessionStore) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	doc, err := s.client.Collection(LlmSessionsCollection).Doc(sessionID).Get(ctx)

	// If it doesn't exist, replicate GetOrCreate behavior by returning an empty, safely initialized session
	if status.Code(err) == codes.NotFound {
		s.logger.Debug("Session not found, creating new instance", "session_id", sessionID)
		return &builder.Session{
			ID:               sessionID,
			AcceptedOverlays: make(map[string]builder.FileState),
			PendingProposals: make(map[string]builder.ChangeProposal),
			UpdatedAt:        time.Now(),
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

	// Ensure maps are never nil to prevent panics during assignment
	if sess.AcceptedOverlays == nil {
		sess.AcceptedOverlays = make(map[string]builder.FileState)
	}
	if sess.PendingProposals == nil {
		sess.PendingProposals = make(map[string]builder.ChangeProposal)
	}

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
