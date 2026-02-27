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
)

// TestFirestoreSessionStore_Integration requires the Firestore Emulator to be running.
// It can be started via: `gcloud emulators firestore start --host-port=127.0.0.1:8080`
// And run via: `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080 go test ./internal/store/...`
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
	// Using a specific bundle collection for the test to avoid collisions
	sessionStore := store.NewFirestoreSessionStore(client, "TestCacheBundles", logger)

	t.Run("Compiled Caches CRUD", func(t *testing.T) {
		baseCacheID := "base-bundle-123"

		// 1. Create a Compiled Cache
		cc := &builder.CompiledCache{
			ID:         "cc-999",
			ExternalID: "cachedContents/gemini-abc",
			Provider:   "gemini",
			AttachmentsUsed: []builder.Attachment{
				{ID: "att-1", CacheID: "base-bundle-123"},
			},
			// Truncate to avoid microsecond rounding issues when reading from Firestore
			CreatedAt: time.Now().Truncate(time.Millisecond),
		}

		err := sessionStore.SaveCompiledCache(ctx, baseCacheID, cc)
		require.NoError(t, err, "Failed to save CompiledCache")

		// 2. Retrieve the caches for the bundle
		caches, err := sessionStore.ListCompiledCaches(ctx, baseCacheID)
		require.NoError(t, err, "Failed to list CompiledCaches")
		require.Len(t, caches, 1, "Expected exactly 1 compiled cache")

		fetched := caches[0]
		assert.Equal(t, "cc-999", fetched.ID)
		assert.Equal(t, "cachedContents/gemini-abc", fetched.ExternalID)
		assert.Equal(t, "gemini", fetched.Provider)
		assert.Len(t, fetched.AttachmentsUsed, 1)
		assert.Equal(t, "att-1", fetched.AttachmentsUsed[0].ID)
		assert.True(t, cc.CreatedAt.Equal(fetched.CreatedAt), "CreatedAt mismatch")
	})

	t.Run("Sessions GetOrCreate and Persistence", func(t *testing.T) {
		sessionID := "test-session-456"

		// 1. Get a Non-Existent Session (Should safely initialize maps)
		sess, err := sessionStore.GetSession(ctx, sessionID)
		require.NoError(t, err, "GetSession should not error on missing documents")
		require.NotNil(t, sess)

		assert.Equal(t, sessionID, sess.ID)
		assert.NotNil(t, sess.AcceptedOverlays, "AcceptedOverlays map must be initialized")
		assert.NotNil(t, sess.PendingProposals, "PendingProposals map must be initialized")

		// 2. Mutate the session state
		sess.CompiledCacheID = "cc-999"
		sess.AcceptedOverlays["main.go"] = builder.FileState{
			Content:   "package main\n\nfunc main() {}",
			IsDeleted: false,
		}

		proposalTime := time.Now().Truncate(time.Millisecond)
		sess.PendingProposals["prop-1"] = builder.ChangeProposal{
			ID:         "prop-1",
			FilePath:   "auth.go",
			NewContent: "package auth",
			Reasoning:  "Updated logic",
			Status:     builder.StatusPending,
			CreatedAt:  proposalTime,
		}

		// 3. Save the mutated session
		err = sessionStore.SaveSession(ctx, sess)
		require.NoError(t, err, "Failed to save Session")

		// 4. Fetch it back and verify persistence
		fetched, err := sessionStore.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, fetched)

		assert.Equal(t, "cc-999", fetched.CompiledCacheID)

		// Verify Maps
		require.Contains(t, fetched.AcceptedOverlays, "main.go")
		assert.Equal(t, "package main\n\nfunc main() {}", fetched.AcceptedOverlays["main.go"].Content)

		require.Contains(t, fetched.PendingProposals, "prop-1")
		prop := fetched.PendingProposals["prop-1"]
		assert.Equal(t, "auth.go", prop.FilePath)
		assert.Equal(t, builder.StatusPending, prop.Status)
		assert.True(t, proposalTime.Equal(prop.CreatedAt), "Proposal CreatedAt mismatch")
	})
}
