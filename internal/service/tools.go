package service

import "google.golang.org/genai"

// GetTools returns the function definitions available to the Gemini model.
func GetTools() []*genai.Tool {
	return []*genai.Tool{
		{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        "propose_change",
					Description: "Propose a modification to a file. This creates a pending change request that the user must review and accept. Do NOT assume the change is applied immediately.",
					Parameters: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"file_path": {
								Type:        genai.TypeString,
								Description: "The relative path of the file to modify (e.g., 'pkg/session/manager.go').",
							},
							"new_content": {
								Type:        genai.TypeString,
								Description: "The FULL content of the file with the changes applied. Do not use diffs or snippets.",
							},
							"reasoning": {
								Type:        genai.TypeString,
								Description: "A short explanation of why this change is necessary and what it fixes.",
							},
						},
						Required: []string{"file_path", "new_content", "reasoning"},
					},
				},
			},
		},
	}
}
