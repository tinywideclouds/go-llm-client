package service

import (
	"testing"

	"google.golang.org/genai"
)

func TestGetTools(t *testing.T) {
	tools := GetWorkspaceTools()

	if len(tools) == 0 {
		t.Fatal("Expected at least one tool definition")
	}

	// Drill down to the specific function
	funcs := tools[0].FunctionDeclarations
	if len(funcs) == 0 {
		t.Fatal("Expected function declarations")
	}

	proposalTool := funcs[0]
	if proposalTool.Name != "propose_change" {
		t.Errorf("Expected tool name 'propose_change', got %s", proposalTool.Name)
	}

	// Verify Schema
	if proposalTool.Parameters.Type != genai.TypeObject {
		t.Error("Parameters should be TypeObject")
	}

	required := proposalTool.Parameters.Required
	if len(required) != 2 {
		t.Errorf("Expected 2 required fields (file_path, reasoning), got %d", len(required))
	}

	// Verify all properties exist
	expectedProps := []string{"file_path", "patch", "new_content", "reasoning"}
	for _, prop := range expectedProps {
		if _, ok := proposalTool.Parameters.Properties[prop]; !ok {
			t.Errorf("Missing expected parameter property: '%s'", prop)
		}
	}
}
