package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/tinywideclouds/go-data-sources/pkg/yaml"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// ErrContextTooLarge is returned when the fetched files exceed the safety memory limits.
var ErrContextTooLarge = errors.New("firestore context exceeds memory limit")

type StoreCollections struct {
	BundleCollection   string
	FilesCollection    string
	ProfilesCollection string
	MaxFetchBytes      int64 // Limit to prevent OOM errors during fetch
}

// Fetcher defines the contract for retrieving context from the database.
type Fetcher interface {
	FetchCacheFiles(ctx context.Context, cacheID urn.URN, profileID *urn.URN) (map[string]string, error)
	Close() error
}

type FirestoreFetcher struct {
	client           *firestore.Client
	storeCollections StoreCollections
	logger           *slog.Logger
}

func NewFirestoreFetcher(client *firestore.Client, storeCollections StoreCollections, logger *slog.Logger) *FirestoreFetcher {
	return &FirestoreFetcher{
		client:           client,
		storeCollections: storeCollections,
		logger:           logger,
	}
}

func (f *FirestoreFetcher) Close() error {
	f.logger.Info("Closing Firestore connection in LLM Service")
	return f.client.Close()
}

func (f *FirestoreFetcher) FetchCacheFiles(ctx context.Context, cacheID urn.URN, profileID *urn.URN) (map[string]string, error) {
	f.logger.Info("Fetching cache files", "cacheID", cacheID.String())

	var activeRules *yaml.FilterRules

	// 1. If a ProfileID is provided, fetch and parse its rules first
	if profileID != nil {
		f.logger.Info("Using profile for fetching", "profileID", profileID.String())
		profileRef := f.client.Collection(f.storeCollections.BundleCollection).Doc(cacheID.String()).Collection(f.storeCollections.ProfilesCollection).Doc(profileID.String())
		doc, err := profileRef.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve profile %s: %w", profileID.String(), err)
		}

		var profileData struct {
			RulesYaml string `firestore:"rulesYaml"`
		}
		if err := doc.DataTo(&profileData); err != nil {
			return nil, fmt.Errorf("failed to parse profile data: %w", err)
		}

		rules, err := yaml.ParseYAML(profileData.RulesYaml)
		if err != nil {
			return nil, fmt.Errorf("failed to parse profile rules yaml: %w", err)
		}
		activeRules = &rules
		f.logger.Debug("Loaded filter profile", "includes", len(rules.Include), "excludes", len(rules.Exclude))
	}

	// Calculate maximum allowed bytes (default to 100MB if not specified)
	limit := f.storeCollections.MaxFetchBytes
	if limit <= 0 {
		limit = 100 * 1024 * 1024 // 100 MB
	}
	var currentBytes int64

	// 2. Stream the files and apply the filter in-memory safely
	filesMap := make(map[string]string)
	filesRef := f.client.Collection(f.storeCollections.BundleCollection).Doc(cacheID.String()).Collection(f.storeCollections.FilesCollection)
	iter := filesRef.Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate cache files: %w", err)
		}

		var fileData struct {
			Path    string `firestore:"path"`
			Content string `firestore:"content"`
		}
		if err := doc.DataTo(&fileData); err != nil {
			f.logger.Warn("Failed to unmarshal file data", "docID", doc.Ref.ID, "error", err)
			continue
		}

		// 3. Apply the Profile Filter (if one exists)
		if activeRules != nil && !activeRules.Match(fileData.Path) {
			continue
		}

		// 4. Memory Safety Guard
		fileSize := int64(len(fileData.Path) + len(fileData.Content))
		if currentBytes+fileSize > limit {
			f.logger.Error("Memory limit exceeded during Firestore fetch", "limit_bytes", limit, "current_bytes", currentBytes, "aborted_file", fileData.Path)
			return nil, fmt.Errorf("%w: > %d bytes", ErrContextTooLarge, limit)
		}

		currentBytes += fileSize
		filesMap[fileData.Path] = fileData.Content
	}

	f.logger.Info("Successfully fetched context files", "cacheID", cacheID.String(), "total_files_loaded", len(filesMap), "total_bytes", currentBytes)
	return filesMap, nil
}
