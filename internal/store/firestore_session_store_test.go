package store_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

func mustURN(s string) urn.URN {
	u, err := urn.Parse(s)
	if err != nil {
		panic("invalid test URN: " + s)
	}
	return u
}

// TestFirestoreSessionStore_Integration requires the Firestore Emulator to be running.
func TestFirestoreSessionStore_Integration(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Firestore SessionStore integration test. Set FIRESTORE_EMULATOR_HOST to run.")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "test-project")
	require.NoError(t, err)
	defer client.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionStore := store.NewFirestoreSessionStore(client, "TestCacheBundles", logger)

	baseCacheID := mustURN("urn:llm:cache:base-bundle-123")
	sessionID := mustURN("urn:llm:session:test-session-queue")

	t.Run("Compiled Caches CRUD", func(t *testing.T) {
		cc := &builder.CompiledCache{
			ID:       mustURN("urn:llm:compiled-cache:cc-999"),
			Provider: "gemini",
			Sources: []builder.Attachment{
				{ID: mustURN("urn:llm:attachment:att-1"), DataSourceID: baseCacheID},
			},
			CreatedAt: time.Now().Truncate(time.Millisecond),
		}

		err := sessionStore.SaveCompiledCache(ctx, baseCacheID, cc)
		require.NoError(t, err)

		caches, err := sessionStore.ListCompiledCaches(ctx, baseCacheID)
		require.NoError(t, err)
		require.Len(t, caches, 1)

		assert.Equal(t, mustURN("urn:llm:compiled-cache:cc-999"), caches[0].ID)
		assert.Equal(t, builder.CompiledCacheProvider("gemini"), caches[0].Provider)
		assert.Equal(t, mustURN("urn:llm:attachment:att-1"), caches[0].Sources[0].ID)
	})

	t.Run("Sessions GetOrCreate and Persistence", func(t *testing.T) {
		testSessID := mustURN("urn:llm:session:test-session-456")

		// 1. Get Non-Existent
		sess, err := sessionStore.GetSession(ctx, testSessID)
		require.NoError(t, err)
		assert.Equal(t, testSessID, sess.ID)

		// 2. Mutate and Save
		sess.CompiledCacheID = mustURN("urn:llm:compiled-cache:cc-999")
		err = sessionStore.SaveSession(ctx, sess)
		require.NoError(t, err)

		// 3. Fetch and Verify
		fetched, err := sessionStore.GetSession(ctx, testSessID)
		require.NoError(t, err)
		assert.Equal(t, mustURN("urn:llm:compiled-cache:cc-999"), fetched.CompiledCacheID)
	})

	t.Run("Ephemeral Queue Lifecycle", func(t *testing.T) {
		propTime := time.Now().Truncate(time.Millisecond)

		prop1 := &builder.ChangeProposal{
			ID:         "prop-1", // Ephemeral IDs remain strings across the boundary
			SessionID:  sessionID,
			FilePath:   "main.go",
			NewContent: "package main",
			CreatedAt:  propTime,
		}

		prop2 := &builder.ChangeProposal{
			ID:        "prop-2",
			SessionID: sessionID,
			FilePath:  "utils.go",
			Patch:     "@@ diff @@",
			CreatedAt: propTime,
		}

		// 1. Save Proposals directly to Session
		err := sessionStore.SaveProposal(ctx, prop1)
		require.NoError(t, err)
		err = sessionStore.SaveProposal(ctx, prop2)
		require.NoError(t, err)

		// 2. Fetch queue
		queue, err := sessionStore.GetProposalsBySession(ctx, sessionID)
		require.NoError(t, err)
		require.Len(t, queue, 2)

		// 3. Delete Proposal
		err = sessionStore.DeleteProposal(ctx, sessionID, "prop-1")
		require.NoError(t, err)

		// 4. Verify queue is emptied of prop-1
		queueAfterDelete, err := sessionStore.GetProposalsBySession(ctx, sessionID)
		require.NoError(t, err)
		require.Len(t, queueAfterDelete, 1)
		assert.Equal(t, "prop-2", queueAfterDelete[0].ID)
	})
}
