package service

import (
	"google.golang.org/genai"
)

// GetWorkspaceTools returns the function declarations that allow the LLM to mutate the workspace.
func GetWorkspaceTools() []*genai.Tool {
	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name: "propose_change",
					Description: "Propose a modification to a file in the workspace. " +
						"For small edits, ALWAYS provide a unified diff in the `patch` field to save time. " +
						"For creating brand new files or rewriting the majority of a file, use the `new_content` field instead.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"file_path": {
								Type:        genai.TypeString,
								Description: "The exact relative path to the file (e.g., 'cmd/main.go').",
							},
							"patch": {
								Type:        genai.TypeString,
								Description: "A standard Unified Diff (.patch) containing ONLY the changes to be applied. MUST include proper context lines.",
							},
							"new_content": {
								Type:        genai.TypeString,
								Description: "The complete, top-to-bottom raw string content of the file. Use ONLY if `patch` is inappropriate (e.g., new file creation).",
							},
							"reasoning": {
								Type:        genai.TypeString,
								Description: "A short, 1-2 sentence explanation of why this change is being made.",
							},
						},
						// Note: patch and new_content are omitted from Required so the LLM can choose between them
						Required: []string{"file_path", "reasoning"},
					},
				},
			},
		},
	}
}
