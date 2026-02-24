package session

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ProposalStatus string

const (
	StatusPending  ProposalStatus = "pending"
	StatusAccepted ProposalStatus = "accepted"
	StatusRejected ProposalStatus = "rejected"
)

func (p ProposalStatus) Validate() error {
	switch p {
	case StatusPending, StatusAccepted, StatusRejected:
		return nil
	}
	return errors.New("invalid proposal status")
}

type FileState struct {
	Content   string `json:"content"`
	IsDeleted bool   `json:"is_deleted"`
}

type ChangeProposal struct {
	ID         string         `json:"id"`
	FilePath   string         `json:"file_path"`
	NewContent string         `json:"new_content"`
	Reasoning  string         `json:"reasoning"`
	Status     ProposalStatus `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Session struct {
	ID               string
	BaseCacheName    string
	AcceptedOverlays map[string]FileState
	PendingProposals map[string]*ChangeProposal
	Mutex            sync.RWMutex
}

func NewSession(id, baseCacheName string) *Session {
	return &Session{
		ID:               id,
		BaseCacheName:    baseCacheName,
		AcceptedOverlays: make(map[string]FileState),
		PendingProposals: make(map[string]*ChangeProposal),
	}
}

func (s *Session) ProposeChange(filePath, newContent, reasoning string) string {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	propID := fmt.Sprintf("prop-%s", uuid.New().String()[:8])
	s.PendingProposals[propID] = &ChangeProposal{
		ID:         propID,
		FilePath:   filePath,
		NewContent: newContent,
		Reasoning:  reasoning,
		Status:     StatusPending,
		CreatedAt:  time.Now(),
	}
	return propID
}

func (s *Session) AcceptChange(proposalID string) error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	prop, exists := s.PendingProposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal %s not found", proposalID)
	}

	prop.Status = StatusAccepted
	s.AcceptedOverlays[prop.FilePath] = FileState{
		Content:   prop.NewContent,
		IsDeleted: false,
	}
	delete(s.PendingProposals, proposalID)
	return nil
}

func (s *Session) RejectChange(proposalID string) error {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()

	prop, exists := s.PendingProposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal %s not found", proposalID)
	}

	prop.Status = StatusRejected
	delete(s.PendingProposals, proposalID)
	return nil
}

func (s *Session) ListPendingProposals() []*ChangeProposal {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()

	var pending []*ChangeProposal
	for _, p := range s.PendingProposals {
		pending = append(pending, p)
	}
	return pending
}

func (s *Session) GetMergedView(filePath string) (string, bool) {
	s.Mutex.RLock()
	defer s.Mutex.RUnlock()

	state, exists := s.AcceptedOverlays[filePath]
	if exists && !state.IsDeleted {
		return state.Content, true
	}
	return "", false
}
