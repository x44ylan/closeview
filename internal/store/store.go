package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Session struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Agent        string `json:"agent"`
	Title        string `json:"title"`
	ProjectPath  string `json:"projectPath"`
	CreatedAt    string `json:"createdAt"`
	ImportedAt   string `json:"importedAt"`
	MessageCount int    `json:"messageCount"`
	RawPath      string `json:"rawPath"`
	SourceHash   string `json:"sourceHash"`
}

type Message struct {
	ID               string  `json:"id"`
	SessionID        string  `json:"sessionId"`
	Sequence         int     `json:"sequence"`
	Role             string  `json:"role"`
	Content          string  `json:"content"`
	CreatedAt        string  `json:"createdAt"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Finish           string  `json:"finish"`
	Cost             float64 `json:"cost"`
	TokensInput      int     `json:"tokensInput"`
	TokensOutput     int     `json:"tokensOutput"`
	TokensReasoning  int     `json:"tokensReasoning"`
	TokensCacheRead  int     `json:"tokensCacheRead"`
	TokensCacheWrite int     `json:"tokensCacheWrite"`
}

type ToolCall struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"`
	Sequence  int    `json:"sequence"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Input     string `json:"input"`
	Output    string `json:"output"`
}

type CodeBlock struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	MessageID string `json:"messageId"`
	Sequence  int    `json:"sequence"`
	Language  string `json:"language"`
	Content   string `json:"content"`
}

type ParseEvent struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

type RawSource struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Hash      string `json:"hash"`
	Content   string `json:"content,omitempty"`
}

type SessionDetail struct {
	Session     Session      `json:"session"`
	Messages    []Message    `json:"messages"`
	ToolCalls   []ToolCall   `json:"toolCalls"`
	CodeBlocks  []CodeBlock  `json:"codeBlocks"`
	ParseEvents []ParseEvent `json:"parseEvents"`
	RawSource   RawSource    `json:"rawSource"`
}

type NewSession struct {
	Source      string
	Provider    string
	Model       string
	Agent       string
	Title       string
	ProjectPath string
	CreatedAt   string
	RawPath     string
	SourceHash  string
	RawContent  string
	Messages    []NewMessage
	ParseEvents []NewParseEvent
}

type NewMessage struct {
	CreatedAt        string
	Role             string
	Content          string
	Provider         string
	Model            string
	Finish           string
	Cost             float64
	TokensInput      int
	TokensOutput     int
	TokensReasoning  int
	TokensCacheRead  int
	TokensCacheWrite int
	ToolCalls        []NewToolCall
	CodeBlocks       []NewCodeBlock
}

type NewToolCall struct {
	Name   string
	Kind   string
	Status string
	Input  string
	Output string
}

type NewCodeBlock struct {
	Language string
	Content  string
}

type NewParseEvent struct {
	Severity string
	Code     string
	Message  string
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	wrapped := &DB{db: db}
	if err := wrapped.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return wrapped, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate(ctx context.Context) error {
	statements := []string{
		`pragma journal_mode = wal`,
		`pragma foreign_keys = on`,
		`create table if not exists sessions (
			id text primary key,
			source text not null,
			provider text not null default '',
			model text not null default '',
			agent text not null default '',
			title text not null,
			project_path text not null default '',
			created_at text not null,
			imported_at text not null,
			message_count integer not null default 0,
			raw_path text not null default '',
			source_hash text not null default ''
		)`,
		`create table if not exists messages (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			sequence integer not null,
			role text not null,
			content text not null,
			created_at text not null,
			provider text not null default '',
			model text not null default '',
			finish text not null default '',
			cost real not null default 0,
			tokens_input integer not null default 0,
			tokens_output integer not null default 0,
			tokens_reasoning integer not null default 0,
			tokens_cache_read integer not null default 0,
			tokens_cache_write integer not null default 0,
			unique(session_id, sequence)
		)`,
		`create table if not exists tool_calls (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			message_id text not null references messages(id) on delete cascade,
			sequence integer not null,
			name text not null,
			kind text not null default 'unknown',
			status text not null default 'unknown',
			input text not null default '',
			output text not null default ''
		)`,
		`create table if not exists code_blocks (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			message_id text not null references messages(id) on delete cascade,
			sequence integer not null,
			language text not null default '',
			content text not null
		)`,
		`create table if not exists parse_events (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			severity text not null,
			code text not null,
			message text not null
		)`,
		`create table if not exists raw_sources (
			id text primary key,
			session_id text not null references sessions(id) on delete cascade,
			path text not null default '',
			hash text not null default '',
			content text not null default ''
		)`,
		`create virtual table if not exists messages_fts using fts5(session_id unindexed, role unindexed, content)`,
		`create index if not exists sessions_imported_at_idx on sessions(imported_at)`,
		`create index if not exists messages_session_idx on messages(session_id, sequence)`,
		`create index if not exists tool_calls_session_idx on tool_calls(session_id, sequence)`,
		`create index if not exists code_blocks_session_idx on code_blocks(session_id, sequence)`,
		`create index if not exists parse_events_session_idx on parse_events(session_id)`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := d.addColumnIfMissing(ctx, "sessions", "source_hash", "text not null default ''"); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{
		{"sessions", "provider", "text not null default ''"},
		{"sessions", "model", "text not null default ''"},
		{"sessions", "agent", "text not null default ''"},
		{"messages", "provider", "text not null default ''"},
		{"messages", "model", "text not null default ''"},
		{"messages", "finish", "text not null default ''"},
		{"messages", "cost", "real not null default 0"},
		{"messages", "tokens_input", "integer not null default 0"},
		{"messages", "tokens_output", "integer not null default 0"},
		{"messages", "tokens_reasoning", "integer not null default 0"},
		{"messages", "tokens_cache_read", "integer not null default 0"},
		{"messages", "tokens_cache_write", "integer not null default 0"},
	} {
		if err := d.addColumnIfMissing(ctx, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if _, err := d.db.ExecContext(ctx, `create unique index if not exists sessions_source_hash_idx on sessions(source_hash) where source_hash <> ''`); err != nil {
		return err
	}
	return nil
}

func (d *DB) addColumnIfMissing(ctx context.Context, table string, column string, definition string) error {
	rows, err := d.db.QueryContext(ctx, `pragma table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `alter table `+table+` add column `+column+` `+definition)
	return err
}

func (d *DB) InsertSession(ctx context.Context, input NewSession) (Session, error) {
	return d.insertSession(ctx, input, false)
}

// ReplaceSession replaces a source archive and its search rows in one transaction.
// A failed insert leaves the previous archive intact.
func (d *DB) ReplaceSession(ctx context.Context, input NewSession) (Session, error) {
	return d.insertSession(ctx, input, true)
}

func (d *DB) insertSession(ctx context.Context, input NewSession, replace bool) (Session, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if input.Source == "" {
		input.Source = "unknown"
	}
	if input.Title == "" {
		input.Title = "Untitled session"
	}
	createdAt := input.CreatedAt
	if createdAt == "" {
		createdAt = now
	}

	session := Session{
		ID:           newID("ses"),
		Source:       input.Source,
		Provider:     input.Provider,
		Model:        input.Model,
		Agent:        input.Agent,
		Title:        input.Title,
		ProjectPath:  input.ProjectPath,
		CreatedAt:    createdAt,
		ImportedAt:   now,
		MessageCount: len(input.Messages),
		RawPath:      input.RawPath,
		SourceHash:   input.SourceHash,
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()

	if replace && input.SourceHash != "" {
		if _, err := tx.ExecContext(ctx, `delete from messages_fts where session_id in (select id from sessions where source_hash = ?)`, input.SourceHash); err != nil {
			return Session{}, err
		}
		if _, err := tx.ExecContext(ctx, `delete from sessions where source_hash = ?`, input.SourceHash); err != nil {
			return Session{}, err
		}
	}

	_, err = tx.ExecContext(ctx, `insert into sessions (id, source, provider, model, agent, title, project_path, created_at, imported_at, message_count, raw_path, source_hash) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Source, session.Provider, session.Model, session.Agent, session.Title, session.ProjectPath, session.CreatedAt, session.ImportedAt, session.MessageCount, session.RawPath, session.SourceHash)
	if err != nil {
		return Session{}, err
	}

	for i, message := range input.Messages {
		role := normalizeRole(message.Role)
		messageID := newID("msg")
		messageCreatedAt := message.CreatedAt
		if messageCreatedAt == "" {
			messageCreatedAt = now
		}
		_, err = tx.ExecContext(ctx, `insert into messages (id, session_id, sequence, role, content, created_at, provider, model, finish, cost, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			messageID, session.ID, i+1, role, message.Content, messageCreatedAt, message.Provider, message.Model, message.Finish, message.Cost, message.TokensInput, message.TokensOutput, message.TokensReasoning, message.TokensCacheRead, message.TokensCacheWrite)
		if err != nil {
			return Session{}, err
		}
		_, err = tx.ExecContext(ctx, `insert into messages_fts (session_id, role, content) values (?, ?, ?)`, session.ID, role, message.Content)
		if err != nil {
			return Session{}, err
		}
		for j, block := range message.CodeBlocks {
			_, err = tx.ExecContext(ctx, `insert into code_blocks (id, session_id, message_id, sequence, language, content) values (?, ?, ?, ?, ?, ?)`,
				newID("code"), session.ID, messageID, j+1, block.Language, block.Content)
			if err != nil {
				return Session{}, err
			}
		}
		for j, call := range message.ToolCalls {
			name := call.Name
			if name == "" {
				name = "tool"
			}
			kind := call.Kind
			if kind == "" {
				kind = "unknown"
			}
			status := call.Status
			if status == "" {
				status = "unknown"
			}
			_, err = tx.ExecContext(ctx, `insert into tool_calls (id, session_id, message_id, sequence, name, kind, status, input, output) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				newID("tool"), session.ID, messageID, j+1, name, kind, status, call.Input, call.Output)
			if err != nil {
				return Session{}, err
			}
		}
	}
	if input.RawContent != "" || input.RawPath != "" || input.SourceHash != "" {
		_, err = tx.ExecContext(ctx, `insert into raw_sources (id, session_id, path, hash, content) values (?, ?, ?, ?, ?)`,
			newID("raw"), session.ID, input.RawPath, input.SourceHash, input.RawContent)
		if err != nil {
			return Session{}, err
		}
	}
	for _, event := range input.ParseEvents {
		severity := event.Severity
		if severity == "" {
			severity = "warning"
		}
		_, err = tx.ExecContext(ctx, `insert into parse_events (id, session_id, severity, code, message) values (?, ?, ?, ?, ?)`,
			newID("event"), session.ID, severity, event.Code, event.Message)
		if err != nil {
			return Session{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (d *DB) ListSessions(ctx context.Context, query string) ([]Session, error) {
	rows, err := d.db.QueryContext(ctx, listSessionsSQL(query), listSessionsArgs(query)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.Source, &session.Provider, &session.Model, &session.Agent, &session.Title, &session.ProjectPath, &session.CreatedAt, &session.ImportedAt, &session.MessageCount, &session.RawPath, &session.SourceHash); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (d *DB) GetSessionWithMessages(ctx context.Context, idOrPrefix string) (Session, []Message, error) {
	detail, err := d.GetSessionDetail(ctx, idOrPrefix)
	if err != nil {
		return Session{}, nil, err
	}
	return detail.Session, detail.Messages, nil
}

func (d *DB) GetSessionDetail(ctx context.Context, idOrPrefix string) (SessionDetail, error) {
	session, err := d.GetSession(ctx, idOrPrefix)
	if err != nil {
		return SessionDetail{}, err
	}
	messages, err := d.listMessages(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	toolCalls, err := d.listToolCalls(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	codeBlocks, err := d.listCodeBlocks(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	parseEvents, err := d.listParseEvents(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	rawSource, err := d.getRawSource(ctx, session.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{Session: session, Messages: messages, ToolCalls: toolCalls, CodeBlocks: codeBlocks, ParseEvents: parseEvents, RawSource: rawSource}, nil
}

func (d *DB) listMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := d.db.QueryContext(ctx, `select id, session_id, sequence, role, content, created_at, provider, model, finish, cost, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write from messages where session_id = ? order by sequence`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.ID, &message.SessionID, &message.Sequence, &message.Role, &message.Content, &message.CreatedAt, &message.Provider, &message.Model, &message.Finish, &message.Cost, &message.TokensInput, &message.TokensOutput, &message.TokensReasoning, &message.TokensCacheRead, &message.TokensCacheWrite); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (d *DB) listToolCalls(ctx context.Context, sessionID string) ([]ToolCall, error) {
	rows, err := d.db.QueryContext(ctx, `select id, session_id, message_id, sequence, name, kind, status, input, output from tool_calls where session_id = ? order by sequence`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var calls []ToolCall
	for rows.Next() {
		var call ToolCall
		if err := rows.Scan(&call.ID, &call.SessionID, &call.MessageID, &call.Sequence, &call.Name, &call.Kind, &call.Status, &call.Input, &call.Output); err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

func (d *DB) listCodeBlocks(ctx context.Context, sessionID string) ([]CodeBlock, error) {
	rows, err := d.db.QueryContext(ctx, `select id, session_id, message_id, sequence, language, content from code_blocks where session_id = ? order by sequence`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var blocks []CodeBlock
	for rows.Next() {
		var block CodeBlock
		if err := rows.Scan(&block.ID, &block.SessionID, &block.MessageID, &block.Sequence, &block.Language, &block.Content); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

func (d *DB) listParseEvents(ctx context.Context, sessionID string) ([]ParseEvent, error) {
	rows, err := d.db.QueryContext(ctx, `select id, session_id, severity, code, message from parse_events where session_id = ? order by id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []ParseEvent
	for rows.Next() {
		var event ParseEvent
		if err := rows.Scan(&event.ID, &event.SessionID, &event.Severity, &event.Code, &event.Message); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (d *DB) getRawSource(ctx context.Context, sessionID string) (RawSource, error) {
	row := d.db.QueryRowContext(ctx, `select id, session_id, path, hash, content from raw_sources where session_id = ? limit 1`, sessionID)
	var source RawSource
	if err := row.Scan(&source.ID, &source.SessionID, &source.Path, &source.Hash, &source.Content); err != nil {
		if err == sql.ErrNoRows {
			return RawSource{}, nil
		}
		return RawSource{}, err
	}
	return source, nil
}

func (d *DB) GetSession(ctx context.Context, idOrPrefix string) (Session, error) {
	rows, err := d.db.QueryContext(ctx, `select id, source, provider, model, agent, title, project_path, created_at, imported_at, message_count, raw_path, source_hash from sessions where id = ? or id like ? order by imported_at desc limit 2`, idOrPrefix, idOrPrefix+"%")
	if err != nil {
		return Session{}, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(&session.ID, &session.Source, &session.Provider, &session.Model, &session.Agent, &session.Title, &session.ProjectPath, &session.CreatedAt, &session.ImportedAt, &session.MessageCount, &session.RawPath, &session.SourceHash); err != nil {
			return Session{}, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return Session{}, err
	}
	if len(sessions) == 0 {
		return Session{}, fmt.Errorf("session %q not found", idOrPrefix)
	}
	if len(sessions) > 1 {
		return Session{}, fmt.Errorf("session prefix %q is ambiguous", idOrPrefix)
	}
	return sessions[0], nil
}

func (d *DB) SessionExistsByHash(ctx context.Context, sourceHash string) (bool, error) {
	if sourceHash == "" {
		return false, nil
	}
	var count int
	if err := d.db.QueryRowContext(ctx, `select count(*) from sessions where source_hash = ?`, sourceHash).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) DeleteSessionByHash(ctx context.Context, sourceHash string) error {
	if sourceHash == "" {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `delete from messages_fts where session_id in (select id from sessions where source_hash = ?)`, sourceHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from sessions where source_hash = ?`, sourceHash); err != nil {
		return err
	}
	return tx.Commit()
}

func listSessionsSQL(query string) string {
	if query == "" {
		return `select id, source, provider, model, agent, title, project_path, created_at, imported_at, message_count, raw_path, source_hash from sessions order by imported_at desc`
	}
	return `select distinct s.id, s.source, s.provider, s.model, s.agent, s.title, s.project_path, s.created_at, s.imported_at, s.message_count, s.raw_path, s.source_hash
		from sessions s
		left join messages m on m.session_id = s.id
		where s.id like ? or s.title like ? or s.source like ? or s.provider like ? or s.model like ? or s.agent like ? or s.project_path like ? or m.content like ?
		order by s.imported_at desc`
}

func listSessionsArgs(query string) []any {
	if query == "" {
		return nil
	}
	like := "%" + query + "%"
	return []any{like, like, like, like, like, like, like, like}
}

func normalizeRole(role string) string {
	switch role {
	case "user", "assistant", "system", "tool":
		return role
	default:
		return "unknown"
	}
}

func newID(prefix string) string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
