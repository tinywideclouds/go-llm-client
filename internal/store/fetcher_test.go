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
	"github.com/tinywideclouds/go-llm/pkg/cache/v1"
)

// TestFirestoreFetcher_Integration requires the Firestore Emulator to be running.
// It can be started via: `gcloud emulators firestore start --host-port=127.0.0.1:8080`
// And run tests via: `FIRESTORE_EMULATOR_HOST=127.0.0.1:8080 go test ./internal/store/...`
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
	fetcher := store.NewFirestoreFetcher(client, cache.StoreCollections{"bundle", "files", "profiles"}, logger)

	cacheID := "test-cache-123"

	// 1. Seed the Emulator Database
	bundleRef := client.Collection("bundle").Doc(cacheID)
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

	// 2. Execute the Fetcher (No Profile Filter)
	files, err := fetcher.FetchCacheFiles(ctx, cacheID, "")
	require.NoError(t, err)

	// 3. Assertions
	assert.Len(t, files, 2)
	assert.Equal(t, "package main", files["main.go"])
	assert.Equal(t, "# Test Repo", files["README.md"])
}
