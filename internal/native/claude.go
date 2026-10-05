package native

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type claudeAdapter struct {
	home string
}

type claudeEnvelope struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	SessionID   string          `json:"sessionId"`
	AgentID     string          `json:"agentId"`
	CWD         string          `json:"cwd"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

type claudeParsed struct {
	detail   Detail
	threadID string
}

func NewClaude(home string) Adapter {
	return &claudeAdapter{home: home}
}

func (a *claudeAdapter) Name() string { return "claude" }

func (a *claudeAdapter) List(ctx context.Context) ([]Session, error) {
	root := filepath.Join(a.home, "projects")
	paths, err := filesUnder(root, func(path string) bool {
		return strings.HasSuffix(strings.ToLower(path), ".jsonl")
	})
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		parsed, err := parseClaudeTranscript(path, false, isClaudeSubagentPath(path))
		if err != nil || parsed.threadID == "" {
			continue
		}
		session := parsed.detail.Session
		session.NativeID = filepath.ToSlash(relative)
		session.ThreadID = parsed.threadID
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (a *claudeAdapter) Get(_ context.Context, nativeID string) (Detail, error) {
	path, err := safeSessionPath(filepath.Join(a.home, "projects"), nativeID)
	if err != nil {
		return Detail{}, err
	}
	parsed, err := parseClaudeTranscript(path, true, isClaudeSubagentPath(path))
	if err != nil {
		if os.IsNotExist(err) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, err
	}
	parsed.detail.Session.NativeID = nativeID
	parsed.detail.Session.ThreadID = parsed.threadID
	if parentID := parsed.detail.Session.ParentThreadID; parentID != "" && filepath.Base(parentID) == parentID && parentID != "." && parentID != ".." {
		parentDir := filepath.Dir(path)
		if filepath.Base(parentDir) == "subagents" {
			parentDir = filepath.Dir(filepath.Dir(parentDir))
		}
		parentPath := filepath.Join(parentDir, parentID+".jsonl")
		if info, statErr := os.Stat(parentPath); statErr == nil && info.Mode().IsRegular() {
			if relative, relErr := filepath.Rel(filepath.Join(a.home, "projects"), parentPath); relErr == nil {
				parsed.detail.Session.ParentID = EncodeID(a.Name(), filepath.ToSlash(relative))
			}
		}
	}
	// Only inspect this thread's subagent files, never the entire project catalog.
	children, _ := os.ReadDir(filepath.Join(filepath.Dir(path), parsed.threadID, "subagents"))
	for _, child := range children {
		if child.Type().IsRegular() && strings.HasSuffix(strings.ToLower(child.Name()), ".jsonl") {
			parsed.detail.Session.ChildCount++
		}
	}
	return parsed.detail, nil
}

func (a *claudeAdapter) Delete(_ context.Context, nativeID string) error {
	root := filepath.Join(a.home, "projects")
	path, err := safeSessionPath(root, nativeID)
	if err != nil {
		return err
	}
	parsed, err := parseClaudeTranscript(path, false, isClaudeSubagentPath(path))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	staged := filepath.Join(filepath.Dir(path), ".closeview-delete-"+filepath.Base(path)+".deleting")
	if err := os.Rename(path, staged); err != nil {
		return err
	}
	restore := true
	defer func() {
		if restore {
			_ = os.Rename(staged, path)
		}
	}()
	// history.jsonl is a shared append-only journal. Native Claude writers do
	// not honor our locks, so replacing it can discard their concurrent writes.
	// Retain its metadata; the viewer lists transcripts, not journal entries.
	if err := os.Remove(staged); err != nil {
		return err
	}
	artifactDir := filepath.Join(filepath.Dir(path), parsed.threadID)
	if parsed.threadID != "" && filepath.Base(artifactDir) == parsed.threadID {
		_ = os.RemoveAll(artifactDir)
	}
	restore = false
	return nil
}

func parseClaudeTranscript(path string, includeContent, isSubagent bool) (claudeParsed, error) {
	info, err := os.Stat(path)
	if err != nil {
		return claudeParsed{}, err
	}
	detail := Detail{Messages: []Message{}, ToolCalls: []ToolCall{}, Warnings: []string{}}
	detail.Session.Provider = "anthropic"
	detail.Session.Origin = "Claude Code"
	detail.Session.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	firstPrompt := ""
	agentID := ""
	parentThreadID := ""
	if isSubagent {
		agentID = strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "agent-")
		parentThreadID = claudeParentThreadID(path)
	}
	messageCount := 0
	pending := make(map[string]int)
	err = jsonLines(path, func(line json.RawMessage) error {
		var envelope claudeEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			detail.Warnings = append(detail.Warnings, "Skipped a malformed transcript line")
			return nil
		}
		if envelope.IsSidechain && !isSubagent {
			return nil
		}
		if envelope.AgentID != "" {
			agentID = envelope.AgentID
		}
		if isSubagent && parentThreadID == "" && envelope.SessionID != "" {
			parentThreadID = envelope.SessionID
		} else if envelope.SessionID != "" {
			detail.Session.ThreadID = envelope.SessionID
		}
		if envelope.CWD != "" {
			detail.Session.ProjectPath = envelope.CWD
		}
		if detail.Session.CreatedAt == "" && envelope.Timestamp != "" {
			detail.Session.CreatedAt = envelope.Timestamp
		}
		if envelope.Type != "user" && envelope.Type != "assistant" {
			return nil
		}
		var source claudeMessage
		if json.Unmarshal(envelope.Message, &source) != nil {
			return nil
		}
		role := normalizeNativeRole(firstNonEmpty(source.Role, envelope.Type))
		text, reasoning, calls, results := claudeContent(source.Content)
		if role == "user" && text != "" && firstPrompt == "" && !looksLikeContext(text) {
			firstPrompt = text
		}
		if text != "" {
			messageCount++
		}
		if source.Model != "" {
			detail.Session.Model = source.Model
		}
		if !includeContent {
			return nil
		}
		if reasoning != "" {
			appendNativeMessage(&detail, Message{ID: envelope.UUID + "-reasoning", Role: "reasoning", Content: reasoning, CreatedAt: envelope.Timestamp, Model: source.Model, Provider: "anthropic"})
		}
		messageID := ""
		if text != "" {
			message := Message{
				ID: envelope.UUID, Role: role, Content: text, CreatedAt: envelope.Timestamp,
				Model: source.Model, Provider: "anthropic", TokensInput: source.Usage.InputTokens,
				TokensOutput: source.Usage.OutputTokens, TokensCacheRead: source.Usage.CacheReadInputTokens,
				TokensCacheWrite: source.Usage.CacheCreationInputTokens,
			}
			appendNativeMessage(&detail, message)
			messageID = detail.Messages[len(detail.Messages)-1].ID
		} else {
			messageID = lastAssistantMessage(&detail)
		}
		for _, call := range calls {
			call.MessageID = messageID
			call.Sequence = len(detail.ToolCalls) + 1
			detail.ToolCalls = append(detail.ToolCalls, call)
			pending[call.ID] = len(detail.ToolCalls) - 1
		}
		for id, output := range results {
			if index, ok := pending[id]; ok {
				detail.ToolCalls[index].Output = output
				detail.ToolCalls[index].Status = "completed"
			}
		}
		return nil
	})
	if err != nil {
		return claudeParsed{}, err
	}
	threadID := detail.Session.ThreadID
	if isSubagent {
		threadID = firstNonEmpty(agentID, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		detail.Session.ParentThreadID = parentThreadID
		detail.Session.Agent = firstNonEmpty(agentID, "sub-agent")
	}
	if threadID == "" {
		threadID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	detail.Session.ThreadID = threadID
	detail.Session.MessageCount = messageCount
	detail.Session.Title = titleFallback("", cleanPrompt(firstPrompt), detail.Session.ProjectPath, threadID)
	return claudeParsed{detail: detail, threadID: threadID}, nil
}

func isClaudeSubagentPath(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.Contains(normalized, "/subagents/") || strings.HasPrefix(filepath.Base(path), "agent-")
}

func claudeParentThreadID(path string) string {
	parent := filepath.Dir(path)
	if filepath.Base(parent) == "subagents" {
		return filepath.Base(filepath.Dir(parent))
	}
	return ""
}

func claudeContent(raw json.RawMessage) (string, string, []ToolCall, map[string]string) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return strings.TrimSpace(plain), "", nil, nil
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		ToolUseID string          `json:"tool_use_id"`
		Input     json.RawMessage `json:"input"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", "", nil, nil
	}
	var texts, reasoning []string
	var calls []ToolCall
	results := make(map[string]string)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) != "" {
				texts = append(texts, block.Text)
			}
		case "thinking":
			if strings.TrimSpace(block.Thinking) != "" {
				reasoning = append(reasoning, block.Thinking)
			}
		case "tool_use":
			calls = append(calls, ToolCall{ID: block.ID, Name: block.Name, Kind: inferNativeToolKind(block.Name), Status: "running", Input: rawText(block.Input)})
		case "tool_result":
			output := rawText(block.Content)
			if block.IsError {
				output = "Error: " + output
			}
			results[block.ToolUseID] = output
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n\n")), strings.TrimSpace(strings.Join(reasoning, "\n\n")), calls, results
}

func claudeSessionLabel(path string) string {
	return fmt.Sprintf("Claude session %s", filepath.Base(path))
}
