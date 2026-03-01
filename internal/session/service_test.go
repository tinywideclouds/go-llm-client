package session_test

import (
	"context"
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

	saveProposalFunc          func(ctx context.Context, proposal *builder.ChangeProposal) error
	getProposalsBySessionFunc func(ctx context.Context, sessionID string) ([]builder.ChangeProposal, error)
	deleteProposalFunc        func(ctx context.Context, sessionID, proposalID string) error

	// Spies
	deletedSessionID  string
	deletedProposalID string
}

func (m *mockSessionStore) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	if m.getSessionFunc != nil {
		return m.getSessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *mockSessionStore) SaveSession(ctx context.Context, session *builder.Session) error {
	return nil
}

func (m *mockSessionStore) SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
	return nil
}

func (m *mockSessionStore) ListCompiledCaches(ctx context.Context, firestoreCacheID string) ([]builder.CompiledCache, error) {
	return nil, nil
}

func (m *mockSessionStore) SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	if m.saveProposalFunc != nil {
		return m.saveProposalFunc(ctx, proposal)
	}
	return nil
}

func (m *mockSessionStore) GetProposalsBySession(ctx context.Context, sessionID string) ([]builder.ChangeProposal, error) {
	if m.getProposalsBySessionFunc != nil {
		return m.getProposalsBySessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *mockSessionStore) DeleteProposal(ctx context.Context, sessionID, proposalID string) error {
	m.deletedSessionID = sessionID
	m.deletedProposalID = proposalID
	if m.deleteProposalFunc != nil {
		return m.deleteProposalFunc(ctx, sessionID, proposalID)
	}
	return nil
}

// --- Tests ---

func TestService_RemoveProposal(t *testing.T) {
	ctx := context.Background()
	mockStore := &mockSessionStore{}
	svc := session.NewService(mockStore)

	err := svc.RemoveProposal(ctx, "sess-1", "prop-1")
	require.NoError(t, err)

	// Verify the ephemeral queue item was deleted
	assert.Equal(t, "sess-1", mockStore.deletedSessionID)
	assert.Equal(t, "prop-1", mockStore.deletedProposalID)
}
