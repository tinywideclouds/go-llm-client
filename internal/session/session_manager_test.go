package session

import (
	"sync"
	"testing"
)

func TestManager_GetOrCreateSession(t *testing.T) {
	m := NewManager()

	// Test 1: Create a new session
	sess1 := m.GetOrCreateSession("sess-1", "cache-1")
	if sess1 == nil {
		t.Fatal("Expected a session, got nil")
	}
	if sess1.ID != "sess-1" {
		t.Errorf("Expected session ID 'sess-1', got '%s'", sess1.ID)
	}
	if sess1.BaseCacheName != "cache-1" {
		t.Errorf("Expected cache ID 'cache-1', got '%s'", sess1.BaseCacheName)
	}

	// Test 2: Retrieve the exact same session
	sess2 := m.GetOrCreateSession("sess-1", "cache-ignored")
	if sess2 != sess1 {
		t.Error("Expected to retrieve the identical session pointer, got a different one")
	}
}

func TestManager_GetSession(t *testing.T) {
	m := NewManager()
	m.GetOrCreateSession("sess-1", "cache-1")

	// Test 1: Retrieve existing session
	sess := m.GetSession("sess-1")
	if sess == nil {
		t.Fatal("Expected to retrieve existing session, got nil")
	}

	// Test 2: Retrieve non-existing session
	missing := m.GetSession("sess-missing")
	if missing != nil {
		t.Fatal("Expected nil for non-existent session, got a session")
	}
}

func TestManager_Concurrency(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup

	// Simulate high-concurrency environment
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()

			sessID := "shared-session"
			if routineID%2 == 0 {
				sessID = "unique-session-" + string(rune(routineID))
			}

			// Concurrent Write/Read attempt
			m.GetOrCreateSession(sessID, "cache-test")
			_ = m.GetSession(sessID)
		}(i)
	}

	wg.Wait()

	shared := m.GetSession("shared-session")
	if shared == nil {
		t.Error("Expected 'shared-session' to have been successfully created during concurrent run")
	}
}
