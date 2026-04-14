package context

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillsDirs defines the ordered fallback hierarchy for skills discovery.
// The first directory that exists in the working directory is used.
var SkillsDirs = []string{
	".feino/skills",
	".claude/skills",
	".gemini/skills",
}

// SkillParameter describes a single input parameter accepted by a skill.
type SkillParameter struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
}

// Skill is a parsed, reusable workflow loaded from a Markdown file with YAML
// frontmatter. The Body contains the raw Markdown instructions that follow the
// frontmatter fence.
type Skill struct {
	Name        string
	Description string
	Parameters  []SkillParameter
	Body        string // raw Markdown body after the closing --- fence
	SourceFile  string // absolute path to the originating .md file
}

// skillFrontmatter is the private struct used to unmarshal the YAML block.
type skillFrontmatter struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Parameters  []SkillParameter `yaml:"parameters"`
}

// parseFrontmatter splits a Markdown file into its YAML frontmatter and body.
// If the file does not start with "---", frontmatter is nil and body is the
// entire file content — this is not an error.
// An unclosed frontmatter block (opening "---" without a matching closing line)
// returns an error.
func parseFrontmatter(data []byte) (fm *skillFrontmatter, body []byte, err error) {
	// Normalise line endings.
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return nil, data, nil
	}

	// Scan for the closing "---" starting from line 1.
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return nil, nil, fmt.Errorf("skills: unclosed frontmatter block (missing closing ---)")
	}

	yamlBlock := bytes.Join(lines[1:closeIdx], []byte("\n"))
	var out skillFrontmatter
	if err := yaml.Unmarshal(yamlBlock, &out); err != nil {
		return nil, nil, fmt.Errorf("skills: invalid frontmatter YAML: %w", err)
	}

	bodyLines := lines[closeIdx+1:]
	bodyBytes := bytes.Join(bodyLines, []byte("\n"))
	return &out, bytes.TrimLeft(bodyBytes, "\n"), nil
}

// parseSkillFile reads a single Markdown file and constructs a Skill.
// It returns an error if the file cannot be read, if the frontmatter is
// malformed, or if the required "name" field is absent.
func parseSkillFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("skills: reading %s: %w", path, err)
	}

	fm, body, err := parseFrontmatter(data)
	if err != nil {
		return Skill{}, fmt.Errorf("skills: parsing %s: %w", path, err)
	}

	if fm == nil || strings.TrimSpace(fm.Name) == "" {
		return Skill{}, fmt.Errorf("skills: %s: frontmatter must include a non-empty 'name' field", path)
	}

	return Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Parameters:  fm.Parameters,
		Body:        string(body),
		SourceFile:  path,
	}, nil
}

// loadSkillsDir reads all *.md files from dir and returns parsed skills.
// Files that fail to parse are logged as warnings and skipped — one bad file
// does not block the rest.
func loadSkillsDir(dir string, logger *slog.Logger) ([]Skill, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("skills: glob in %s: %w", dir, err)
	}

	var skills []Skill
	for _, path := range matches {
		s, err := parseSkillFile(path)
		if err != nil {
			logger.Warn("skipping skill file", "path", path, "error", err)
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// discoverSkillsDir returns the first entry in SkillsDirs that exists as a
// directory under workingDir. Returns ("", false) if none are found.
func discoverSkillsDir(workingDir string) (string, bool) {
	for _, d := range SkillsDirs {
		full := filepath.Join(workingDir, d)
		info, err := os.Stat(full)
		if err == nil && info.IsDir() {
			return d, true
		}
	}
	return "", false
}
