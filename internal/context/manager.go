package context

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/odinnordico/feino/internal/constants"
	"github.com/odinnordico/feino/internal/tools"
)

//go:embed prompt.tmpl
var rawPromptTemplate string

// systemTmpl is parsed once at startup; a parse error is a programming error.
var systemTmpl = template.Must(template.New("system").Parse(rawPromptTemplate))

// promptTool is the template view of a single registered tool.
type promptTool struct {
	Name        string
	Description string
}

// promptData holds every value interpolated into the system prompt template.
type promptData struct {
	// Runtime environment
	WorkingDir string
	OS         string
	Arch       string
	Shell      string
	Date       string

	// User profile (empty string → section omitted)
	UserProfile string

	// Agent-learned memories (empty string → section omitted)
	AgentMemories string

	// User / project instructions (empty string → section omitted)
	GlobalInstructions  string
	ProjectFile         string
	ProjectInstructions string

	// Registered capabilities
	Tools  []promptTool
	Skills []Skill

	// Budget-limited semantic code context (pre-rendered string)
	CodeContext string
}

// renderPrompt executes the system prompt template against data.
func renderPrompt(data promptData) (string, error) {
	var buf bytes.Buffer
	if err := systemTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("context: render prompt template: %w", err)
	}
	return buf.String(), nil
}

// ContextFiles defines the strict fallback hierarchy for context detection.
var ContextFiles = []string{
	constants.FeinoMD,
	constants.GeminiMD,
	constants.ClaudeMd,
}

// Manager defines the interface for mapping and learning environment rules.
type Manager interface {
	AutoDetect() bool
	GetActiveFile() string
	GetSystemPrompt() (string, error)
	AppendLearning(learning string) error

	// Lifecycle Context methods
	AddCodeContext(ctx context.Context, path string, source []byte) error
	SetTools(ts []tools.Tool)
	LoadSkills() error
	AssembleContext(ctx context.Context, maxBudget int) (string, error)
}

// FileSystemContextManager handles local workspace interactions for tracking instructions.
type FileSystemContextManager struct {
	mu               sync.RWMutex
	logger           *slog.Logger
	workingDir       string
	activeFile       string
	globalConfigPath string
	parser           *TreeSitterParser
	codeChunks       []SemanticChunk
	tools            []tools.Tool
	skills           []Skill

	// User profile and agent memory — injected by the TUI/REPL at startup.
	userProfile string      // pre-formatted by config.UserProfileConfig.FormatPrompt()
	memoryStore memoryStore // may be nil when no store is configured
}

// memoryStore is the subset of memory.Store used by the context manager.
// Defined as a local interface to avoid an import cycle with internal/memory.
type memoryStore interface {
	FormatPrompt() (string, error)
}

// ManagerOption configures a FileSystemContextManager.
type ManagerOption func(*FileSystemContextManager)

// WithContextLogger sets the logger used by the context manager and its parser.
func WithContextLogger(logger *slog.Logger) ManagerOption {
	return func(m *FileSystemContextManager) {
		m.logger = logger
	}
}

// WithGlobalConfigPath overrides the default global config path (~/.feino/config.md).
// Set to an empty string to disable loading the global config entirely.
func WithGlobalConfigPath(path string) ManagerOption {
	return func(m *FileSystemContextManager) {
		m.globalConfigPath = path
	}
}

// WithUserProfile sets the pre-formatted user profile string that is injected
// into every system prompt as a <user_profile> block. Pass the result of
// config.UserProfileConfig.FormatPrompt(). A non-empty string enables the block.
func WithUserProfile(profile string) ManagerOption {
	return func(m *FileSystemContextManager) {
		m.userProfile = profile
	}
}

// WithMemoryStore attaches a memory store to the context manager. When set,
// FormatPrompt is called each turn and injected as an <agent_memories> block.
func WithMemoryStore(store memoryStore) ManagerOption {
	return func(m *FileSystemContextManager) {
		m.memoryStore = store
	}
}

// NewFileSystemContextManager creates a new context manager aiming at the target directory.
func NewFileSystemContextManager(workingDir string, opts ...ManagerOption) *FileSystemContextManager {
	home, _ := os.UserHomeDir()
	m := &FileSystemContextManager{
		logger:           slog.Default(),
		workingDir:       workingDir,
		globalConfigPath: filepath.Join(home, ".feino", "config.md"),
		codeChunks:       make([]SemanticChunk, 0),
		tools:            make([]tools.Tool, 0),
	}
	for _, opt := range opts {
		opt(m)
	}
	m.parser = NewTreeSitterParser(m.logger)
	return m
}

// AutoDetect attempts to find an existing context file using the strict hierarchy prioritized layout.
func (m *FileSystemContextManager) AutoDetect() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, file := range ContextFiles {
		path := filepath.Join(m.workingDir, file)
		if _, err := os.Stat(path); err == nil {
			m.activeFile = file
			m.logger.Debug("context: detected project context file", "file", file, "path", path)
			return true
		}
	}
	m.activeFile = ""
	m.logger.Debug("context: no project context file found", "working_dir", m.workingDir)
	return false
}

func (m *FileSystemContextManager) GetActiveFile() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeFile
}

// AddCodeContext parses a file and adds its semantic chunks to the session index.
func (m *FileSystemContextManager) AddCodeContext(ctx context.Context, path string, source []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chunks, err := m.parser.ExtractSemanticChunks(ctx, source, path, false)
	if err != nil {
		m.logger.Error("context: failed to extract semantic chunks", "path", path, "error", err)
		return err
	}

	m.codeChunks = append(m.codeChunks, chunks...)
	m.logger.Debug("context: added code context", "path", path, "chunks", len(chunks), "total_chunks", len(m.codeChunks))
	return nil
}

// SetTools updates the list of available tools for context assembly.
func (m *FileSystemContextManager) SetTools(ts []tools.Tool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = ts
}

// LoadSkills discovers the first valid skills directory under the working
// directory and loads all *.md skill files from it. If no skills directory
// is found the call is a no-op and returns nil.
func (m *FileSystemContextManager) LoadSkills() error {
	m.mu.RLock()
	workingDir := m.workingDir
	m.mu.RUnlock()

	dir, ok := discoverSkillsDir(workingDir)
	if !ok {
		m.logger.Debug("context: no skills directory found", "working_dir", workingDir)
		return nil
	}

	skills, err := loadSkillsDir(filepath.Join(workingDir, dir), m.logger)
	if err != nil {
		m.logger.Error("context: failed to load skills", "dir", dir, "error", err)
		return fmt.Errorf("context: loading skills from %s: %w", dir, err)
	}

	m.mu.Lock()
	m.skills = skills
	m.mu.Unlock()

	m.logger.Debug("context: loaded skills", "dir", dir, "count", len(skills))
	return nil
}

// AssembleContext renders the system prompt template with all runtime context
// injected, respecting maxBudget characters for the codebase context section.
// All file I/O happens outside the lock.
func (m *FileSystemContextManager) AssembleContext(ctx context.Context, maxBudget int) (string, error) {
	m.mu.RLock()
	globalConfigPath := m.globalConfigPath
	activeFile := m.activeFile
	workingDir := m.workingDir
	toolsCopy := m.tools
	skillsCopy := m.skills
	chunksCopy := m.codeChunks
	userProfile := m.userProfile
	memStore := m.memoryStore
	m.mu.RUnlock()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = os.Getenv("COMSPEC") // Windows fallback
	}
	if shell == "" {
		shell = "unknown"
	}

	data := promptData{
		WorkingDir: workingDir,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Shell:      shell,
		Date:       time.Now().Format("2006-01-02"),
		Skills:     skillsCopy,
	}

	// User profile from config (set via wizard or /profile).
	data.UserProfile = userProfile

	// Agent-learned memories from the memory store (written by memory_write tool).
	if memStore != nil {
		if mem, err := memStore.FormatPrompt(); err != nil {
			m.logger.Warn("context: failed to load agent memories", "error", err)
		} else {
			data.AgentMemories = mem
		}
	}

	// Global user instructions from ~/.feino/config.md.
	if globalConfigPath != "" {
		if raw, err := os.ReadFile(globalConfigPath); err == nil {
			data.GlobalInstructions = strings.TrimSpace(string(raw))
		}
	}

	// Project instructions: FEINO.md › GEMINI.md › CLAUDE.md.
	if activeFile != "" {
		path := filepath.Join(workingDir, activeFile)
		if raw, err := os.ReadFile(path); err == nil {
			data.ProjectFile = activeFile
			data.ProjectInstructions = strings.TrimSpace(string(raw))
		}
	}

	// Convert tools to template-friendly structs.
	data.Tools = make([]promptTool, len(toolsCopy))
	for i, t := range toolsCopy {
		data.Tools[i] = promptTool{Name: t.GetName(), Description: t.GetDescription()}
	}

	// First pass: render without code context to measure fixed overhead.
	base, err := renderPrompt(data)
	if err != nil {
		return "", err
	}

	// Fill remaining budget with code chunks, then do a second render.
	if len(chunksCopy) > 0 {
		remaining := maxBudget - len(base)
		if remaining <= 0 {
			m.logger.Warn("context: instructions exceed budget, skipping code context",
				"base_size", len(base), "budget", maxBudget)
			return base, nil
		}
		data.CodeContext = buildCodeContext(chunksCopy, remaining, m.logger)
		if data.CodeContext != "" {
			return renderPrompt(data)
		}
	}

	return base, nil
}

// buildCodeContext formats semantic chunks into a string up to budget characters.
func buildCodeContext(chunks []SemanticChunk, budget int, logger *slog.Logger) string {
	var sb strings.Builder
	for i, chunk := range chunks {
		entry := fmt.Sprintf("## %s (%s)\n%s\n\n", chunk.Name, chunk.FilePath, chunk.Content)
		if sb.Len()+len(entry) > budget {
			omitted := len(chunks) - i
			fmt.Fprintf(&sb, "// [TRUNCATED] %d chunks omitted to fit budget.\n", omitted)
			logger.Debug("context: code context truncated", "shown", i, "omitted", omitted)
			break
		}
		sb.WriteString(entry)
	}
	return strings.TrimSpace(sb.String())
}

// GetSystemPrompt reads the entirety of the discovered context file.
func (m *FileSystemContextManager) GetSystemPrompt() (string, error) {
	m.mu.RLock()
	active := m.activeFile
	workingDir := m.workingDir
	m.mu.RUnlock()

	if active == "" {
		return "", errors.New("no active context file detected")
	}

	path := filepath.Join(workingDir, active)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// AppendLearning appends a bullet point to the "## Agent Learnings" section of
// the active context file, creating the section if it does not exist.
func (m *FileSystemContextManager) AppendLearning(learning string) error {
	m.mu.RLock()
	active := m.activeFile
	workingDir := m.workingDir
	m.mu.RUnlock()

	if active == "" {
		return errors.New("no active context file")
	}

	path := filepath.Join(workingDir, active)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	m.logger.Debug("context: appending learning", "file", active)

	content := string(raw)
	const learningsHeader = "## Agent Learnings"
	newBullet := fmt.Sprintf("- %s", strings.TrimSpace(learning))

	var updated string
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != learningsHeader {
			continue
		}

		// Find the insertion point: end of this section (before next ## heading).
		insertionPoint := i + 1
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "##") {
				break
			}
			insertionPoint = j + 1
		}

		merged := make([]string, 0, len(lines)+1)
		merged = append(merged, lines[:insertionPoint]...)
		merged = append(merged, newBullet)
		merged = append(merged, lines[insertionPoint:]...)
		updated = strings.Join(merged, "\n")
		return atomicWriteFile(path, []byte(updated), 0o600)
	}

	// Section not found — append it.
	updated = content + "\n\n" + learningsHeader + "\n" + newBullet + "\n"
	return atomicWriteFile(path, []byte(updated), 0o600)
}

// atomicWriteFile writes data to path using a temp-file-rename so that a crash
// mid-write never leaves a partial file. The temp file is created in the same
// directory as path so that os.Rename is guaranteed to be atomic (same device).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".feino-ctx-*.tmp")
	if err != nil {
		return fmt.Errorf("context: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("context: write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("context: chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("context: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("context: rename temp file: %w", err)
	}
	return nil
}
