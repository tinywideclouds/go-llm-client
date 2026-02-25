package store

import (
	"context"
	"fmt"
	"log/slog"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	// Adjust this import path if you moved it to go-microservice-base
	"github.com/tinywideclouds/go-llm-client/internal/filter"
)

const (
	BundleCollection   = "CacheBundles"
	FilesCollection    = "Files"
	ProfilesCollection = "FilterProfiles"
)

// Fetcher defines the contract for retrieving context from the database.
type Fetcher interface {
	// FetchCacheFiles retrieves all text content for a given cache, optionally filtered by a profile.
	// Returns a map where the key is the file path and the value is the raw file content.
	FetchCacheFiles(ctx context.Context, cacheID, profileID string) (map[string]string, error)
	Close() error
}

type FirestoreFetcher struct {
	client *firestore.Client
	logger *slog.Logger
}

// NewFirestoreFetcher initializes the database client for the LLM service.
func NewFirestoreFetcher(client *firestore.Client, logger *slog.Logger) *FirestoreFetcher {
	return &FirestoreFetcher{
		client: client,
		logger: logger,
	}
}

func (f *FirestoreFetcher) Close() error {
	f.logger.Info("Closing Firestore connection in LLM Service")
	return f.client.Close()
}

func (f *FirestoreFetcher) FetchCacheFiles(ctx context.Context, cacheID, profileID string) (map[string]string, error) {
	f.logger.Info("Fetching cache files", "cacheID", cacheID, "profileID", profileID)

	var activeRules *filter.FilterRules

	// 1. If a ProfileID is provided, fetch and parse its rules first
	if profileID != "" {
		profileRef := f.client.Collection(BundleCollection).Doc(cacheID).Collection(ProfilesCollection).Doc(profileID)
		doc, err := profileRef.Get(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve profile %s: %w", profileID, err)
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
	filesRef := f.client.Collection(BundleCollection).Doc(cacheID).Collection(FilesCollection)
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
			continue // Skip this file, it doesn't match the LLM context profile
		}

		filesMap[fileData.Path] = fileData.Content
	}

	f.logger.Info("Successfully fetched context files", "cacheID", cacheID, "total_files_loaded", len(filesMap))
	return filesMap, nil
}
