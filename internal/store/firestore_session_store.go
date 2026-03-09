// firestore_session_store.go
package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

const (
	LlmSessionsCollection = "LlmSessions"
	CompiledCachesSubCol  = "CompiledCaches"
	ProposalsSubCol       = "Proposals"
)

type FirestoreSessionStore struct {
	client    *firestore.Client
	bundleCol string
	logger    *slog.Logger
}

func NewFirestoreSessionStore(client *firestore.Client, bundleCol string, logger *slog.Logger) *FirestoreSessionStore {
	return &FirestoreSessionStore{
		client:    client,
		bundleCol: bundleCol,
		logger:    logger,
	}
}

// We map the strict domain struct to a primitive struct for Firestore
type firestoreAttachment struct {
	ID        string  `firestore:"id"`
	CacheID   string  `firestore:"cacheId"`
	ProfileID *string `firestore:"profileId,omitempty"`
}

type firestoreCache struct {
	Provider        string                `firestore:"provider"`
	AttachmentsUsed []firestoreAttachment `firestore:"attachmentsUsed"`
	CreatedAt       time.Time             `firestore:"createdAt"`
	ExpiresAt       time.Time             `firestore:"expiresAt"`
}

// --- Compiled Caches ---

func (s *FirestoreSessionStore) SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error {

	fsAttachments := make([]firestoreAttachment, 0, len(cache.AttachmentsUsed))
	for _, att := range cache.AttachmentsUsed {
		fsAtt := firestoreAttachment{
			ID:      att.ID.String(),
			CacheID: att.CacheID.String(),
		}
		if att.ProfileID != nil {
			pid := att.ProfileID.String()
			fsAtt.ProfileID = &pid
		}
		fsAttachments = append(fsAttachments, fsAtt)
	}

	fsCache := firestoreCache{
		Provider:        string(cache.Provider),
		AttachmentsUsed: fsAttachments,
		CreatedAt:       cache.CreatedAt,
		ExpiresAt:       cache.ExpiresAt,
	}

	ref := s.client.Collection(s.bundleCol).Doc(firestoreCacheID.String()).Collection(CompiledCachesSubCol).Doc(cache.ID.String())
	_, err := ref.Set(ctx, fsCache)
	if err != nil {
		return fmt.Errorf("failed to save compiled cache: %w", err)
	}
	return nil
}

func (s *FirestoreSessionStore) ListCompiledCaches(ctx context.Context, firestoreCacheID urn.URN) ([]builder.CompiledCache, error) {
	var caches []builder.CompiledCache
	iter := s.client.Collection(s.bundleCol).Doc(firestoreCacheID.String()).Collection(CompiledCachesSubCol).Documents(ctx)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate compiled caches: %w", err)
		}

		// Read into intermediate struct with strings
		var fsCache struct {
			Provider        string                `firestore:"provider"`
			CreatedAt       time.Time             `firestore:"createdAt"`
			ExpiresAt       time.Time             `firestore:"expiresAt"`
			AttachmentsUsed []firestoreAttachment `firestore:"attachmentsUsed"`
		}

		if err := doc.DataTo(&fsCache); err != nil {
			s.logger.Warn("Failed to unmarshal compiled cache", "docID", doc.Ref.ID, "error", err)
			continue
		}

		id, err := urn.Parse(doc.Ref.ID)
		if err != nil {
			s.logger.Warn("Invalid URN ID for compiled cache", "docID", doc.Ref.ID)
			continue
		}

		c := builder.CompiledCache{
			ID:        id,
			Provider:  builder.CompiledCacheProvider(fsCache.Provider),
			CreatedAt: fsCache.CreatedAt,
			ExpiresAt: fsCache.ExpiresAt,
		}

		for _, att := range fsCache.AttachmentsUsed {
			attID, _ := urn.Parse(att.ID)
			cacheID, _ := urn.Parse(att.CacheID)
			domainAtt := builder.Attachment{
				ID:      attID,
				CacheID: cacheID,
			}
			if att.ProfileID != nil {
				pid, _ := urn.Parse(*att.ProfileID)
				domainAtt.ProfileID = &pid
			}
			c.AttachmentsUsed = append(c.AttachmentsUsed, domainAtt)
		}

		caches = append(caches, c)
	}
	return caches, nil
}

// --- Sessions ---

func (s *FirestoreSessionStore) GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error) {
	doc, err := s.client.Collection(LlmSessionsCollection).Doc(sessionID.String()).Get(ctx)

	if status.Code(err) == codes.NotFound {
		s.logger.Debug("Session not found, creating new instance", "session_id", sessionID.String())
		return &builder.Session{
			ID:        sessionID,
			UpdatedAt: time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var fsSess struct {
		CompiledCacheID string    `firestore:"compiledCacheId"`
		UpdatedAt       time.Time `firestore:"updatedAt"`
	}

	if err := doc.DataTo(&fsSess); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	id, _ := urn.Parse(doc.Ref.ID)
	ccID, _ := urn.Parse(fsSess.CompiledCacheID)

	return &builder.Session{
		ID:              id,
		CompiledCacheID: ccID,
		UpdatedAt:       fsSess.UpdatedAt,
	}, nil
}

func (s *FirestoreSessionStore) SaveSession(ctx context.Context, session *builder.Session) error {
	session.UpdatedAt = time.Now()
	ref := s.client.Collection(LlmSessionsCollection).Doc(session.ID.String())

	// Map to Firestore struct
	fsSess := struct {
		CompiledCacheID string    `firestore:"compiledCacheId"`
		UpdatedAt       time.Time `firestore:"updatedAt"`
	}{
		CompiledCacheID: session.CompiledCacheID.String(),
		UpdatedAt:       session.UpdatedAt,
	}

	_, err := ref.Set(ctx, fsSess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

// --- Ephemeral Proposal Queue ---

func (s *FirestoreSessionStore) SaveProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	ref := s.client.Collection(LlmSessionsCollection).Doc(proposal.SessionID.String()).Collection(ProposalsSubCol).Doc(proposal.ID)

	fsProp := struct {
		SessionID  string    `firestore:"sessionId"`
		FilePath   string    `firestore:"filePath"`
		Patch      string    `firestore:"patch,omitempty"`
		NewContent string    `firestore:"newContent,omitempty"`
		Reasoning  string    `firestore:"reasoning"`
		CreatedAt  time.Time `firestore:"createdAt"`
	}{
		SessionID:  proposal.SessionID.String(),
		FilePath:   proposal.FilePath,
		Patch:      proposal.Patch,
		NewContent: proposal.NewContent,
		Reasoning:  proposal.Reasoning,
		CreatedAt:  proposal.CreatedAt,
	}

	_, err := ref.Set(ctx, fsProp)
	if err != nil {
		return fmt.Errorf("failed to save proposal: %w", err)
	}
	return nil
}

func (s *FirestoreSessionStore) GetProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error) {
	var proposals []builder.ChangeProposal
	iter := s.client.Collection(LlmSessionsCollection).Doc(sessionID.String()).Collection(ProposalsSubCol).Documents(ctx)

	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list proposals by session: %w", err)
		}

		var fsProp struct {
			SessionID  string    `firestore:"sessionId"`
			FilePath   string    `firestore:"filePath"`
			Patch      string    `firestore:"patch,omitempty"`
			NewContent string    `firestore:"newContent,omitempty"`
			Reasoning  string    `firestore:"reasoning"`
			CreatedAt  time.Time `firestore:"createdAt"`
		}

		if err := doc.DataTo(&fsProp); err != nil {
			continue
		}

		sid, _ := urn.Parse(fsProp.SessionID)

		proposals = append(proposals, builder.ChangeProposal{
			ID:         doc.Ref.ID,
			SessionID:  sid,
			FilePath:   fsProp.FilePath,
			Patch:      fsProp.Patch,
			NewContent: fsProp.NewContent,
			Reasoning:  fsProp.Reasoning,
			CreatedAt:  fsProp.CreatedAt,
		})
	}
	return proposals, nil
}

func (s *FirestoreSessionStore) DeleteProposal(ctx context.Context, sessionID urn.URN, proposalID string) error {
	ref := s.client.Collection(LlmSessionsCollection).Doc(sessionID.String()).Collection(ProposalsSubCol).Doc(proposalID)
	_, err := ref.Delete(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete proposal: %w", err)
	}
	return nil
}
