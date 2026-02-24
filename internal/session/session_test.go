package session

import (
	"testing"
)

func TestProposalStatusValidation(t *testing.T) {
	tests := []struct {
		status  ProposalStatus
		isValid bool
	}{
		{StatusPending, true},
		{StatusAccepted, true},
		{StatusRejected, true},
		{"invalid_status", false},
	}

	for _, tt := range tests {
		err := tt.status.Validate()
		if (err == nil) != tt.isValid {
			t.Errorf("Validate(%s) error = %v, wantValid %v", tt.status, err, tt.isValid)
		}
	}
}

func TestSessionInitialization(t *testing.T) {
	s := NewSession("sess-123", "cache-abc")

	if s.ID != "sess-123" {
		t.Errorf("Expected ID sess-123, got %s", s.ID)
	}
	if s.AcceptedOverlays == nil {
		t.Error("AcceptedOverlays map should be initialized")
	}
	if s.PendingProposals == nil {
		t.Error("PendingProposals map should be initialized")
	}
}

func TestSessionWorkflow(t *testing.T) {
	session := NewSession("test-session", "cache-v1")
	propID := session.ProposeChange("main.go", "func main() { fmt.Println(\"Hello\") }", "Fixing typo")

	if propID == "" {
		t.Fatal("Expected a valid Proposal ID")
	}

	props := session.ListPendingProposals()
	if len(props) != 1 {
		t.Errorf("Expected 1 pending proposal, got %d", len(props))
	}
	if props[0].Status != StatusPending {
		t.Errorf("Expected status Pending, got %s", props[0].Status)
	}

	_, exists := session.GetMergedView("main.go")
	if exists {
		t.Error("Proposal should not be in merged view before acceptance")
	}

	err := session.AcceptChange(propID)
	if err != nil {
		t.Fatalf("Failed to accept change: %v", err)
	}

	content, exists := session.GetMergedView("main.go")
	if !exists {
		t.Error("Accepted change should be in merged view")
	}
	if content != "func main() { fmt.Println(\"Hello\") }" {
		t.Errorf("Content mismatch. Got: %s", content)
	}

	props = session.ListPendingProposals()
	if len(props) != 0 {
		t.Error("Pending proposals should be empty after acceptance")
	}
}

func TestRejectChange(t *testing.T) {
	session := NewSession("test-session-2", "cache-v1")
	propID := session.ProposeChange("utils.go", "broken code", "bad idea")

	err := session.RejectChange(propID)
	if err != nil {
		t.Fatalf("Failed to reject change: %v", err)
	}

	_, exists := session.GetMergedView("utils.go")
	if exists {
		t.Error("Rejected change should not be in merged view")
	}

	props := session.ListPendingProposals()
	if len(props) != 0 {
		t.Error("Pending proposals should be empty after rejection")
	}
}
