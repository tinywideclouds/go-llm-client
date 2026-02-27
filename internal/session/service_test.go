package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// --- Mocks ---

type mockSessionStore struct {
	getSessionFunc        func(ctx context.Context, sessionID string) (*builder.Session, error)
	saveSessionFunc       func(ctx context.Context, session *builder.Session) error
	saveCompiledCacheFunc func(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error
	savedSession          *builder.Session
}

func (m *mockSessionStore) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	if m.getSessionFunc != nil {
		return m.getSessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *mockSessionStore) SaveSession(ctx context.Context, session *builder.Session) error {
	m.savedSession = session
	if m.saveSessionFunc != nil {
		return m.saveSessionFunc(ctx, session)
	}
	return nil
}

func (m *mockSessionStore) SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
	if m.saveCompiledCacheFunc != nil {
		return m.saveCompiledCacheFunc(ctx, firestoreCacheID, cache)
	}
	return nil
}

func (m *mockSessionStore) ListCompiledCaches(ctx context.Context, firestoreCacheID string) ([]builder.CompiledCache, error) {
	return nil, nil
}

// --- Tests ---

func TestService_AcceptProposal(t *testing.T) {
	ctx := context.Background()

	t.Run("Success - Transitions to Accepted and creates Overlay", func(t *testing.T) {
		mockStore := &mockSessionStore{
			getSessionFunc: func(ctx context.Context, sessionID string) (*builder.Session, error) {
				return &builder.Session{
					ID:               "sess-1",
					AcceptedOverlays: make(map[string]builder.FileState),
					PendingProposals: map[string]builder.ChangeProposal{
						"prop-1": {
							ID:         "prop-1",
							FilePath:   "main.go",
							NewContent: "package main",
							Status:     builder.StatusPending,
						},
					},
				}, nil
			},
		}

		svc := session.NewService(mockStore)

		err := svc.AcceptProposal(ctx, "sess-1", "prop-1")
		require.NoError(t, err)

		// Verify that the store's SaveSession was called with the mutated state
		require.NotNil(t, mockStore.savedSession)

		prop := mockStore.savedSession.PendingProposals["prop-1"]
		assert.Equal(t, builder.StatusAccepted, prop.Status)

		overlay, exists := mockStore.savedSession.AcceptedOverlays["main.go"]
		require.True(t, exists)
		assert.Equal(t, "package main", overlay.Content)
		assert.False(t, overlay.IsDeleted)
	})

	t.Run("Error - Proposal Not Found", func(t *testing.T) {
		mockStore := &mockSessionStore{
			getSessionFunc: func(ctx context.Context, sessionID string) (*builder.Session, error) {
				return &builder.Session{
					ID:               "sess-1",
					PendingProposals: make(map[string]builder.ChangeProposal),
				}, nil
			},
		}

		svc := session.NewService(mockStore)
		err := svc.AcceptProposal(ctx, "sess-1", "prop-1")
		assert.ErrorContains(t, err, "proposal not found")
	})

	t.Run("Error - Proposal Already Accepted", func(t *testing.T) {
		mockStore := &mockSessionStore{
			getSessionFunc: func(ctx context.Context, sessionID string) (*builder.Session, error) {
				return &builder.Session{
					ID: "sess-1",
					PendingProposals: map[string]builder.ChangeProposal{
						"prop-1": {Status: builder.StatusAccepted},
					},
				}, nil
			},
		}

		svc := session.NewService(mockStore)
		err := svc.AcceptProposal(ctx, "sess-1", "prop-1")
		assert.ErrorContains(t, err, "proposal is no longer pending")
	})
}

func TestService_RejectProposal(t *testing.T) {
	ctx := context.Background()

	t.Run("Success - Transitions to Rejected", func(t *testing.T) {
		mockStore := &mockSessionStore{
			getSessionFunc: func(ctx context.Context, sessionID string) (*builder.Session, error) {
				return &builder.Session{
					ID: "sess-1",
					PendingProposals: map[string]builder.ChangeProposal{
						"prop-1": {
							ID:     "prop-1",
							Status: builder.StatusPending,
						},
					},
				}, nil
			},
		}

		svc := session.NewService(mockStore)

		err := svc.RejectProposal(ctx, "sess-1", "prop-1")
		require.NoError(t, err)

		require.NotNil(t, mockStore.savedSession)
		assert.Equal(t, builder.StatusRejected, mockStore.savedSession.PendingProposals["prop-1"].Status)
		// Should not add to overlays
		assert.Empty(t, mockStore.savedSession.AcceptedOverlays)
	})
}

func TestService_StorePassThroughs(t *testing.T) {
	ctx := context.Background()
	mockStore := &mockSessionStore{
		getSessionFunc: func(ctx context.Context, sessionID string) (*builder.Session, error) {
			return &builder.Session{ID: sessionID}, nil
		},
		saveCompiledCacheFunc: func(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
			if firestoreCacheID == "err-cache" {
				return errors.New("store error")
			}
			return nil
		},
	}
	svc := session.NewService(mockStore)

	t.Run("GetSession", func(t *testing.T) {
		sess, err := svc.GetSession(ctx, "sess-123")
		require.NoError(t, err)
		assert.Equal(t, "sess-123", sess.ID)
	})

	t.Run("SaveCompiledCache", func(t *testing.T) {
		err := svc.SaveCompiledCache(ctx, "cache-1", &builder.CompiledCache{})
		assert.NoError(t, err)

		err = svc.SaveCompiledCache(ctx, "err-cache", &builder.CompiledCache{})
		assert.ErrorContains(t, err, "store error")
	})
}
