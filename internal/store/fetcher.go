package store

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/tinywideclouds/go-llm/pkg/cache/v1"
	"github.com/tinywideclouds/go-llm/pkg/yaml/filter"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// Fetcher defines the contract for retrieving context from the database.
type Fetcher interface {
	FetchCacheFiles(ctx context.Context, cacheID urn.URN, profileID *urn.URN) (map[string]string, error)
	Close() error
}

type FirestoreFetcher struct {
	client           *firestore.Client
	storeCollections cache.StoreCollections
	logger           *slog.Logger
}

func NewFirestoreFetcher(client *firestore.Client, storeCollections cache.StoreCollections, logger *slog.Logger) *FirestoreFetcher {
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

	var activeRules *filter.FilterRules

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

		rules, err := filter.ParseYAML(profileData.RulesYaml)
		if err != nil {
			return nil, fmt.Errorf("failed to parse profile rules yaml: %w", err)
		}
		activeRules = &rules
		f.logger.Debug("Loaded filter profile", "includes", len(rules.Include), "excludes", len(rules.Exclude))
	}

	// 2. Stream the files and apply the filter in-memory
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

		filesMap[fileData.Path] = fileData.Content
	}

	f.logger.Info("Successfully fetched context files", "cacheID", cacheID.String(), "total_files_loaded", len(filesMap))
	return filesMap, nil
}
