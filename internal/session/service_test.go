package session_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// --- Test Helpers ---

func mustURN(s string) urn.URN {
	u, err := urn.Parse(s)
	if err != nil {
		panic("invalid test URN: " + s)
	}
	return u
}

// --- Mocks ---

type mockSessionStore struct {
	getSessionFunc        func(ctx context.Context, sessionID urn.URN) (*builder.Session, error)
	saveSessionFunc       func(ctx context.Context, session *builder.Session) error
	saveCompiledCacheFunc func(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error

	saveProposalFunc          func(ctx context.Context, proposal *builder.ChangeProposal) error
	getProposalsBySessionFunc func(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error)
	deleteProposalFunc        func(ctx context.Context, sessionID urn.URN, proposalID string) error

	// Spies
	deletedSessionID  urn.URN
	deletedProposalID string
}

func (m *mockSessionStore) GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error) {
	if m.getSessionFunc != nil {
		return m.getSessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *mockSessionStore) SaveSession(ctx context.Context, session *builder.Session) error {
	return nil
}

func (m *mockSessionStore) SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error {
	return nil
}

func (m *mockSessionStore) ListCompiledCaches(ctx context.Context, firestoreCacheID urn.URN) ([]builder.CompiledCache, error) {
	return nil, nil
}

func (m *mockSessionStore) SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	if m.saveProposalFunc != nil {
		return m.saveProposalFunc(ctx, proposal)
	}
	return nil
}

func (m *mockSessionStore) GetProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error) {
	if m.getProposalsBySessionFunc != nil {
		return m.getProposalsBySessionFunc(ctx, sessionID)
	}
	return nil, nil
}

func (m *mockSessionStore) DeleteProposal(ctx context.Context, sessionID urn.URN, proposalID string) error {
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

	testSessID := mustURN("urn:llm:session:sess-1")

	err := svc.RemoveProposal(ctx, testSessID, "prop-1")
	require.NoError(t, err)

	// Verify the ephemeral queue item was deleted using strict types
	assert.Equal(t, testSessID, mockStore.deletedSessionID)
	assert.Equal(t, "prop-1", mockStore.deletedProposalID)
}
