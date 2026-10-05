package native

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenCodeListGetDelete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`pragma foreign_keys = on`,
		`create table session (id text primary key, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table message (id text primary key, session_id text references session(id), data text, time_created integer)`,
		`create table part (id text primary key, session_id text references session(id), message_id text references message(id), data text, time_created integer)`,
		`insert into session values ('ses-test', 'Fix login', '/tmp/project', 'build', '{"id":"claude-test","providerID":"anthropic"}', 1700000000000, 1700000001000)`,
		`insert into message values ('msg-test', 'ses-test', '{"role":"user"}', 1700000000000)`,
		`insert into part values ('part-test', 'ses-test', 'msg-test', '{"type":"text","text":"Hello OpenCode"}', 1700000000000)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := NewOpenCode(path)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("list: sessions=%+v err=%v", sessions, err)
	}
	if sessions[0].ThreadID != "ses-test" || sessions[0].MessageCount != 1 {
		t.Fatalf("unexpected summary: %+v", sessions[0])
	}
	detail, err := adapter.Get(context.Background(), "ses-test")
	if err != nil || len(detail.Messages) != 1 || detail.Messages[0].Content != "Hello OpenCode" {
		t.Fatalf("get: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), "ses-test"); err != nil {
		t.Fatal(err)
	}
	sessions, err = adapter.List(context.Background())
	if err != nil || len(sessions) != 0 {
		t.Fatalf("session remained after delete: %+v err=%v", sessions, err)
	}
}

func TestOpenCodeRelationships(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`create table session (id text primary key, parent_id text, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table message (id text primary key, session_id text)`,
		`insert into session values ('parent', '', 'Parent session', '/tmp/project', '', '', 1, 2)`,
		`insert into session values ('child', 'parent', 'Child session', '/tmp/project', 'explore', '', 2, 3)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	catalog := New(NewOpenCode(path)).List(context.Background(), "", "")
	parent := catalogSession(t, catalog, "parent")
	child := catalogSession(t, catalog, "child")
	if child.ParentThreadID != "parent" || child.ParentID != parent.ID || !child.IsSubsession || parent.ChildCount != 1 {
		t.Fatalf("relationships not linked: parent=%+v child=%+v", parent, child)
	}
	filteredParent := catalogSession(t, New(NewOpenCode(path)).List(context.Background(), "Parent session", ""), "parent")
	if filteredParent.ChildCount != 1 {
		t.Fatalf("filtered parent lost child count: %+v", filteredParent)
	}
	filteredChild := catalogSession(t, New(NewOpenCode(path)).List(context.Background(), "Child session", ""), "child")
	if filteredChild.ParentID != parent.ID {
		t.Fatalf("filtered child lost parent link: %+v", filteredChild)
	}
}

func TestOpenCodeV2ListGetAndDeleteThroughService(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`create table session_v2 (id text primary key, parent_id text, title text, directory text, agent text, model text, time_created integer, time_updated integer)`,
		`create table session_message (id text primary key, session_id text, type text, seq integer, time_created integer, data text)`,
		`insert into session_v2 values ('ses-v2', null, 'V2 session', '/tmp/v2', 'build', '{"providerID":"openai","id":"gpt-test"}', 1700000000000, 1700000001000)`,
		`insert into session_message values ('msg-user', 'ses-v2', 'user', 1, 1700000000000, '{"text":"Hello v2"}')`,
		`insert into session_message values ('msg-assistant', 'ses-v2', 'assistant', 2, 1700000001000, '{"model":{"providerID":"openai","id":"gpt-test"},"content":[{"type":"text","text":"Ready"},{"type":"tool","name":"bash","state":{"status":"completed","input":{"command":"true"},"output":"ok"}}]}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	deleted := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "opencode" || password != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		deleted = strings.TrimPrefix(r.URL.Path, "/api/session/")
		store, err := sql.Open("sqlite", path)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer store.Close()
		if _, err := store.Exec(`delete from session_v2 where id=?`, deleted); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	serviceFile := filepath.Join(root, "service.json")
	if err := os.WriteFile(serviceFile, []byte(`{"url":"`+server.URL+`","password":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOSEVIEW_OPENCODE_URL", "")
	t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD", "")
	t.Setenv("CLOSEVIEW_OPENCODE_PASSWORD_FILE", serviceFile)

	adapter := NewOpenCode(path)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 1 || sessions[0].MessageCount != 2 {
		t.Fatalf("list v2: sessions=%+v err=%v", sessions, err)
	}
	detail, err := adapter.Get(context.Background(), "ses-v2")
	if err != nil || len(detail.Messages) != 2 || detail.Messages[0].Content != "Hello v2" || len(detail.ToolCalls) != 1 {
		t.Fatalf("get v2: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), "ses-v2"); err != nil {
		t.Fatal(err)
	}
	if deleted != "ses-v2" {
		t.Fatalf("v2 deletion did not reach service: %q", deleted)
	}
}

func TestClaudeListGetDelete(t *testing.T) {
	home := t.TempDir()
	projectDir := filepath.Join(home, "projects", "-tmp-project")
	if err := os.MkdirAll(filepath.Join(projectDir, "claude-test", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(projectDir, "claude-test.jsonl")
	writeFixture(t, transcript, strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:05Z","message":{"role":"user","content":"Inspect the code"}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:06Z","message":{"role":"assistant","model":"claude-test-model","content":[{"type":"text","text":"I found it"},{"type":"tool_use","id":"tool-1","name":"Read","input":{"file_path":"main.go"}}],"usage":{"input_tokens":10,"output_tokens":4}}}`,
		`{"type":"user","uuid":"u2","sessionId":"claude-test","cwd":"/tmp/project","timestamp":"2026-01-02T03:04:07Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"package main"}]}}`,
	}, "\n")+"\n")
	writeFixture(t, filepath.Join(home, "history.jsonl"), `{"sessionId":"claude-test","display":"Inspect the code"}`+"\n")
	writeFixture(t, filepath.Join(projectDir, "claude-test", "subagents", "agent-worker.jsonl"), strings.Join([]string{
		`{"type":"user","uuid":"su1","sessionId":"claude-test","agentId":"worker","isSidechain":true,"cwd":"/tmp/project","timestamp":"2026-01-02T03:04:08Z","message":{"role":"user","content":"Inspect the parser"}}`,
		`{"type":"assistant","uuid":"sa1","sessionId":"claude-test","agentId":"worker","isSidechain":true,"cwd":"/tmp/project","timestamp":"2026-01-02T03:04:09Z","message":{"role":"assistant","model":"claude-test-model","content":"Parser looks good"}}`,
	}, "\n")+"\n")

	adapter := NewClaude(home)
	sessions, err := adapter.List(context.Background())
	if err != nil || len(sessions) != 2 {
		t.Fatalf("list: sessions=%+v err=%v", sessions, err)
	}
	parent := nativeSession(t, sessions, "claude-test")
	child := nativeSession(t, sessions, "worker")
	if parent.MessageCount != 2 || child.ParentThreadID != "claude-test" || child.Agent != "worker" || child.MessageCount != 2 {
		t.Fatalf("unexpected Claude relationship: parent=%+v child=%+v", parent, child)
	}
	detail, err := adapter.Get(context.Background(), parent.NativeID)
	if err != nil || len(detail.Messages) != 2 || len(detail.ToolCalls) != 1 || !strings.Contains(detail.ToolCalls[0].Output, "package main") {
		t.Fatalf("get: detail=%+v err=%v", detail, err)
	}
	if err := adapter.Delete(context.Background(), parent.NativeID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "claude-test")); !os.IsNotExist(err) {
		t.Fatalf("session artifact directory still exists: %v", err)
	}
	history, err := os.ReadFile(filepath.Join(home, "history.jsonl"))
	if err != nil || !strings.Contains(string(history), "claude-test") {
		t.Fatalf("shared Claude history metadata must remain intact: %v", err)
	}
}

func nativeSession(t *testing.T, sessions []Session, threadID string) Session {
	t.Helper()
	for _, session := range sessions {
		if session.ThreadID == threadID {
			return session
		}
	}
	t.Fatalf("session %q not found in %+v", threadID, sessions)
	return Session{}
}

func catalogSession(t *testing.T, catalog Catalog, nativeID string) Session {
	t.Helper()
	for _, session := range catalog.Sessions {
		if session.NativeID == nativeID {
			return session
		}
	}
	t.Fatalf("session %q not found in %+v", nativeID, catalog.Sessions)
	return Session{}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileDoesNotContain(t *testing.T, path, value string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), value) {
		t.Fatalf("%s still contains %q", path, value)
	}
}
