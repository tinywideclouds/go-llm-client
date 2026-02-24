package session

import (
	"sync"
)

// Manager handles the lifecycle, storage, and thread-safe access of active LLM sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager initializes a ready-to-use Session Manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// GetOrCreateSession retrieves an existing session by ID, or initializes and stores
// a new one if it does not currently exist.
func (m *Manager) GetOrCreateSession(sessID, cacheID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, exists := m.sessions[sessID]
	if !exists {
		// Calls the unified constructor from session.go
		sess = NewSession(sessID, cacheID)
		m.sessions[sessID] = sess
	}

	return sess
}

// GetSession safely retrieves a session if it exists, returning nil if it does not.
func (m *Manager) GetSession(sessID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[sessID]
}
