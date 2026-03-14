package store_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/internal/store"
)

func TestFirestoreFetcher_Integration(t *testing.T) {
	emulatorHost := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		t.Skip("Skipping Firestore integration test. Set FIRESTORE_EMULATOR_HOST to run.")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "test-project")
	require.NoError(t, err)
	defer client.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cacheID := mustURN("urn:llm:cache:test-cache-123")

	// 1. Seed the Emulator Database
	bundleRef := client.Collection("bundle").Doc(cacheID.String())
	_, err = bundleRef.Set(ctx, map[string]string{"status": "ready"})
	require.NoError(t, err)

	filesRef := bundleRef.Collection("files")
	_, err = filesRef.Doc("file1").Set(ctx, map[string]interface{}{
		"path":    "main.go",
		"content": "package main",
	})
	require.NoError(t, err)

	_, err = filesRef.Doc("file2").Set(ctx, map[string]interface{}{
		"path":    "README.md",
		"content": "# Test Repo",
	})
	require.NoError(t, err)

	t.Run("Successfully fetches context within memory limits", func(t *testing.T) {
		fetcher := store.NewFirestoreFetcher(client, store.StoreCollections{
			BundleCollection:   "bundle",
			FilesCollection:    "files",
			ProfilesCollection: "profiles",
			MaxFetchBytes:      1024 * 1024, // 1 MB limit (plenty for this test)
		}, logger)

		files, err := fetcher.FetchCacheFiles(ctx, cacheID, nil)
		require.NoError(t, err)

		assert.Len(t, files, 2)
		assert.Equal(t, "package main", files["main.go"])
		assert.Equal(t, "# Test Repo", files["README.md"])
	})

	t.Run("Fails with ErrContextTooLarge when hitting memory limits", func(t *testing.T) {
		fetcher := store.NewFirestoreFetcher(client, store.StoreCollections{
			BundleCollection:   "bundle",
			FilesCollection:    "files",
			ProfilesCollection: "profiles",
			MaxFetchBytes:      10, // Artificially low 10 byte limit
		}, logger)

		files, err := fetcher.FetchCacheFiles(ctx, cacheID, nil)

		require.ErrorIs(t, err, store.ErrContextTooLarge)
		require.Nil(t, files)
	})
}
