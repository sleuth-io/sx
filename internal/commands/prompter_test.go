package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// MockPrompter implements Prompter for testing with expect-style responses
type MockPrompter struct {
	// responses is a queue of expected prompts and their responses
	responses []mockResponse
	index     int
}

type mockResponse struct {
	expectContains string // Expected substring in the prompt message
	response       string // Response to return
	isConfirm      bool   // Whether this is a confirm prompt
}

// NewMockPrompter creates a new mock prompter with no expectations
func NewMockPrompter() *MockPrompter {
	return &MockPrompter{
		responses: []mockResponse{},
	}
}

// ExpectPrompt adds an expectation for a prompt containing the given text
func (m *MockPrompter) ExpectPrompt(contains, response string) *MockPrompter {
	m.responses = append(m.responses, mockResponse{
		expectContains: contains,
		response:       response,
		isConfirm:      false,
	})
	return m
}

// ExpectConfirm adds an expectation for a confirmation prompt
func (m *MockPrompter) ExpectConfirm(contains string, confirmed bool) *MockPrompter {
	response := "n"
	if confirmed {
		response = "y"
	}
	m.responses = append(m.responses, mockResponse{
		expectContains: contains,
		response:       response,
		isConfirm:      true,
	})
	return m
}

// Prompt implements Prompter
func (m *MockPrompter) Prompt(message string) (string, error) {
	if m.index >= len(m.responses) {
		return "", fmt.Errorf("unexpected prompt: %s (no more responses configured)", message)
	}

	expected := m.responses[m.index]
	if !strings.Contains(message, expected.expectContains) {
		return "", fmt.Errorf("prompt mismatch: expected message containing %q, got %q", expected.expectContains, message)
	}

	m.index++
	return expected.response, nil
}

// PromptWithDefault implements Prompter
func (m *MockPrompter) PromptWithDefault(message, defaultValue string) (string, error) {
	response, err := m.Prompt(message)
	if err != nil {
		return "", err
	}
	if response == "" {
		return defaultValue, nil
	}
	return response, nil
}

// Confirm implements Prompter
func (m *MockPrompter) Confirm(message string) (bool, error) {
	response, err := m.Prompt(message)
	if err != nil {
		return false, err
	}
	response = strings.ToLower(response)
	return response == "y" || response == "yes", nil
}

// AssertAllUsed verifies that all expected prompts were called
func (m *MockPrompter) AssertAllUsed() error {
	if m.index < len(m.responses) {
		return fmt.Errorf("not all expected prompts were used: %d/%d used", m.index, len(m.responses))
	}
	return nil
}

// ExecuteWithPrompter executes a cobra command with a mock prompter injected
// Returns any error from command execution or prompt assertion
func ExecuteWithPrompter(cmd *cobra.Command, prompter *MockPrompter) error {
	ctx := WithPrompter(context.Background(), prompter)
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		return err
	}

	return prompter.AssertAllUsed()
}
