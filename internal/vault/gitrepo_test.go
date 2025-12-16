package vault

import (
	"strings"
	"testing"
)

func TestExtractTemplateVersion(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		prefix   string
		expected string
	}{
		{
			name: "install.sh with version",
			content: `#!/bin/bash
# Template version: 5
echo "hello"`,
			prefix:   "# Template version: ",
			expected: "5",
		},
		{
			name: "README.md with version",
			content: `# Title
<!-- Template version: 10 -->
Some content`,
			prefix:   "<!-- Template version: ",
			expected: "10",
		},
		{
			name: "no version found",
			content: `#!/bin/bash
echo "hello"`,
			prefix:   "# Template version: ",
			expected: "",
		},
		{
			name: "version with extra whitespace after colon",
			content: `#!/bin/bash
# Template version:   42
echo "hello"`,
			prefix:   "# Template version: ",
			expected: "42",
		},
		{
			name: "version in middle of file",
			content: `line 1
line 2
<!-- Template version: 7 -->
line 4`,
			prefix:   "<!-- Template version: ",
			expected: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTemplateVersion(tt.content, tt.prefix)
			if result != tt.expected {
				t.Errorf("extractTemplateVersion() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestShouldUpdateTemplate(t *testing.T) {
	tests := []struct {
		name            string
		fileVersion     string
		templateVersion string
		expected        bool
		shouldPanic     bool
	}{
		{
			name:            "no file version (treat as 0) - should update",
			fileVersion:     "",
			templateVersion: "1",
			expected:        true,
		},
		{
			name:            "file version 1, template version 2 - should update",
			fileVersion:     "1",
			templateVersion: "2",
			expected:        true,
		},
		{
			name:            "file version 5, template version 10 - should update",
			fileVersion:     "5",
			templateVersion: "10",
			expected:        true,
		},
		{
			name:            "file version 2, template version 2 - no update",
			fileVersion:     "2",
			templateVersion: "2",
			expected:        false,
		},
		{
			name:            "file version 5, template version 3 - no downgrade",
			fileVersion:     "5",
			templateVersion: "3",
			expected:        false,
		},
		{
			name:            "invalid file version (treat as 0) - should update",
			fileVersion:     "invalid",
			templateVersion: "1",
			expected:        true,
		},
		{
			name:            "missing template version - should panic",
			fileVersion:     "1",
			templateVersion: "",
			shouldPanic:     true,
		},
		{
			name:            "invalid template version - should panic",
			fileVersion:     "1",
			templateVersion: "invalid",
			shouldPanic:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("shouldUpdateTemplate() should have panicked but didn't")
					}
				}()
			}

			result := shouldUpdateTemplate(tt.fileVersion, tt.templateVersion)

			if !tt.shouldPanic && result != tt.expected {
				t.Errorf("shouldUpdateTemplate(%q, %q) = %v, want %v", tt.fileVersion, tt.templateVersion, result, tt.expected)
			}
		})
	}
}

func TestGenerateInstallScript(t *testing.T) {
	repoURL := "https://github.com/test/repo.git"
	result := generateInstallScript(repoURL)

	// Check that the template was rendered
	if result == "" {
		t.Error("generateInstallScript() returned empty string")
	}

	// Check that REPO_URL was replaced
	if !strings.Contains(result, repoURL) {
		t.Errorf("generateInstallScript() should contain repo URL %q", repoURL)
	}

	// Check that template version was injected
	if !strings.Contains(result, "# Template version: "+installScriptTemplateVersion) {
		t.Errorf("generateInstallScript() should contain version %q", installScriptTemplateVersion)
	}

	// Check that placeholders were replaced
	if strings.Contains(result, "{{.REPO_URL}}") {
		t.Error("generateInstallScript() still contains {{.REPO_URL}} placeholder")
	}
	if strings.Contains(result, "{{.TEMPLATE_VERSION}}") {
		t.Error("generateInstallScript() still contains {{.TEMPLATE_VERSION}} placeholder")
	}
}

func TestGenerateReadme(t *testing.T) {
	repoURL := "https://github.com/test/repo.git"
	result := generateReadme(repoURL)

	// Check that the template was rendered
	if result == "" {
		t.Error("generateReadme() returned empty string")
	}

	// Check that template version was injected
	if !strings.Contains(result, "<!-- Template version: "+readmeTemplateVersion+" -->") {
		t.Errorf("generateReadme() should contain version %q", readmeTemplateVersion)
	}

	// Check that install URL was generated (should be raw GitHub URL)
	expectedURL := "https://raw.githubusercontent.com/test/repo/main/install.sh"
	if !strings.Contains(result, expectedURL) {
		t.Errorf("generateReadme() should contain install URL %q, got:\n%s", expectedURL, result)
	}

	// Check that placeholders were replaced
	if strings.Contains(result, "{{.INSTALL_URL}}") {
		t.Error("generateReadme() still contains {{.INSTALL_URL}} placeholder")
	}
	if strings.Contains(result, "{{.TEMPLATE_VERSION}}") {
		t.Error("generateReadme() still contains {{.TEMPLATE_VERSION}} placeholder")
	}
}

func TestConvertToRawURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected string
	}{
		{
			name:     "HTTPS URL with .git",
			repoURL:  "https://github.com/test/repo.git",
			expected: "https://raw.githubusercontent.com/test/repo/main/install.sh",
		},
		{
			name:     "HTTPS URL without .git",
			repoURL:  "https://github.com/test/repo",
			expected: "https://raw.githubusercontent.com/test/repo/main/install.sh",
		},
		{
			name:     "SSH URL",
			repoURL:  "git@github.com:test/repo.git",
			expected: "https://raw.githubusercontent.com/test/repo/main/install.sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToRawURL(tt.repoURL)
			if result != tt.expected {
				t.Errorf("convertToRawURL(%q) = %q, want %q", tt.repoURL, result, tt.expected)
			}
		})
	}
}
