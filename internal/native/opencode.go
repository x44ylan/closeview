package native

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Dylan-Liew/closeview/internal/parser/opencode"
	"github.com/Dylan-Liew/closeview/internal/store"
	_ "modernc.org/sqlite"
)

type openCodeAdapter struct {
	path string
}

func NewOpenCode(path string) Adapter {
	return &openCodeAdapter{path: path}
}

func (a *openCodeAdapter) Name() string { return "opencode" }

func (a *openCodeAdapter) List(ctx context.Context) ([]Session, error) {
	return a.summaries(ctx, "")
}

// summaries reads one session for detail views; only catalog requests scan all.
func (a *openCodeAdapter) summaries(ctx context.Context, nativeID string) ([]Session, error) {
	if _, err := os.Stat(a.path); err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+a.path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	sources, err := opencode.SessionSources(ctx, db)
	if err != nil {
		return nil, err
	}
	var queries []string
	var args []any
	for _, source := range sources {
		filter, children, parentExists := "", "0", "0"
		if nativeID != "" {
			filter = ` where s.id = ?`
			args = append(args, nativeID)
			if source.Parent != "''" {
				children = `(select count(*) from ` + source.Table + ` c where c.parent_id = s.id)`
				parentExists = `exists(select 1 from ` + source.Table + ` p where p.id = ` + source.Parent + `)`
			}
		}
		queries = append(queries, `
		select s.id as id, `+source.Parent+`, coalesce(s.title, ''), coalesce(s.directory, ''),
			coalesce(s.agent, ''), coalesce(s.model, ''),
			coalesce(s.time_created, 0), coalesce(s.time_updated, 0) as updated,
			(select count(*) from `+source.Messages+` m where m.session_id = s.id), `+children+`, `+parentExists+`
		from `+source.Table+` s`+filter)
	}
	rows, err := db.QueryContext(ctx, strings.Join(queries, " union all ")+` order by updated desc, id desc`, args...)
	if err != nil {
		return nil, fmt.Errorf("read OpenCode sessions: %w", err)
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var id, parentID, title, project, agent, modelData string
		var created, updated int64
		var count, children, parentExists int
		if err := rows.Scan(&id, &parentID, &title, &project, &agent, &modelData, &created, &updated, &count, &children, &parentExists); err != nil {
			return nil, err
		}
		provider, model := openCodeModel(modelData)
		session := Session{
			NativeID: id, ThreadID: id, ParentThreadID: parentID, Title: titleFallback(title, "", project, id),
			ProjectPath: project, Agent: agent, Provider: provider, Model: model,
			CreatedAt: sourceTime(created), UpdatedAt: sourceTime(updated),
			MessageCount: count, ChildCount: children,
		}
		if parentExists != 0 {
			session.ParentID = EncodeID(a.Name(), parentID)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (a *openCodeAdapter) Get(ctx context.Context, nativeID string) (Detail, error) {
	sessions, warnings, err := opencode.ParseDB(ctx, a.path, opencode.Options{SessionID: nativeID})
	if err != nil {
		return Detail{}, err
	}
	for _, session := range sessions {
		if session.SourceHash == "opencode-db:"+nativeID {
			detail := detailFromNewSession(session, nativeID)
			detail.Warnings = append(detail.Warnings, warnings...)
			listed, listErr := a.summaries(ctx, nativeID)
			if listErr == nil {
				for _, summary := range listed {
					if summary.NativeID == nativeID {
						detail.Session = summary
						break
					}
				}
			}
			return detail, nil
		}
	}
	return Detail{}, ErrNotFound
}

func (a *openCodeAdapter) Delete(ctx context.Context, nativeID string) error {
	if strings.TrimSpace(nativeID) == "" {
		return fmt.Errorf("OpenCode session id is required")
	}
	db, err := sql.Open("sqlite", "file:"+a.path+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var hasV2 int
	if err := db.QueryRowContext(ctx, `select count(*) from sqlite_master where type='table' and name='session_v2'`).Scan(&hasV2); err != nil {
		return err
	}
	if hasV2 > 0 {
		var exists int
		if err := db.QueryRowContext(ctx, `select count(*) from session_v2 where id=?`, nativeID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		if err := deleteOpenCodeV2(ctx, nativeID); err != nil {
			return err
		}
		if err := db.QueryRowContext(ctx, `select count(*) from session_v2 where id=?`, nativeID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return fmt.Errorf("OpenCode reported deletion but the session remains in its local store; check that CloseView and OpenCode use the same database")
		}
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `select count(*) from session where id = ?`, nativeID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	tables, err := sessionOwnedTables(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, `delete from `+quoteIdentifier(table)+` where session_id = ?`, nativeID); err != nil {
			return fmt.Errorf("delete OpenCode %s records: %w", table, err)
		}
	}
	result, err := tx.ExecContext(ctx, `delete from session where id = ?`, nativeID)
	if err != nil {
		return fmt.Errorf("delete OpenCode session: %w", err)
	}
	deleted, _ := result.RowsAffected()
	if deleted != 1 {
		return fmt.Errorf("expected to delete one OpenCode session, deleted %d", deleted)
	}
	return tx.Commit()
}

func sessionOwnedTables(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `select name from sqlite_master where type = 'table' and name <> 'session' and name not like 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		candidates = append(candidates, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var tables []string
	for _, table := range candidates {
		columns, err := tx.QueryContext(ctx, `pragma table_info(`+quoteIdentifier(table)+`)`)
		if err != nil {
			return nil, err
		}
		hasSessionID := false
		for columns.Next() {
			var cid, notNull, pk int
			var name, columnType string
			var defaultValue sql.NullString
			if err := columns.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
				columns.Close()
				return nil, err
			}
			if name == "session_id" {
				hasSessionID = true
			}
		}
		if err := columns.Close(); err != nil {
			return nil, err
		}
		if hasSessionID {
			tables = append(tables, table)
		}
	}
	sort.SliceStable(tables, func(i, j int) bool {
		priority := func(table string) int {
			switch table {
			case "part":
				return 0
			case "message":
				return 2
			default:
				return 1
			}
		}
		return priority(tables[i]) < priority(tables[j])
	})
	return tables, nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sourceTime(value int64) string {
	if value <= 0 {
		return ""
	}
	var instant time.Time
	switch {
	case value > 1e17:
		instant = time.Unix(0, value)
	case value > 1e14:
		instant = time.Unix(0, value*1000)
	case value > 1e11:
		instant = time.UnixMilli(value)
	default:
		instant = time.Unix(value, 0)
	}
	return instant.UTC().Format(time.RFC3339Nano)
}

func detailFromNewSession(input store.NewSession, nativeID string) Detail {
	detail := Detail{
		Session: Session{
			NativeID: nativeID, ThreadID: nativeID, Title: titleFallback(input.Title, firstPrompt(input.Messages), input.ProjectPath, nativeID),
			ProjectPath: input.ProjectPath, CreatedAt: input.CreatedAt, Provider: input.Provider,
			Model: input.Model, Agent: input.Agent, MessageCount: len(input.Messages),
		},
		Messages: []Message{}, ToolCalls: []ToolCall{},
	}
	for index, message := range input.Messages {
		messageID := fmt.Sprintf("message-%d", index+1)
		detail.Messages = append(detail.Messages, Message{
			ID: messageID, Sequence: index + 1, Role: message.Role, Content: message.Content,
			CreatedAt: message.CreatedAt, Provider: message.Provider, Model: message.Model,
			Finish: message.Finish, Cost: message.Cost, TokensInput: message.TokensInput,
			TokensOutput: message.TokensOutput, TokensReasoning: message.TokensReasoning,
			TokensCacheRead: message.TokensCacheRead, TokensCacheWrite: message.TokensCacheWrite,
		})
		for toolIndex, call := range message.ToolCalls {
			detail.ToolCalls = append(detail.ToolCalls, ToolCall{
				ID: fmt.Sprintf("tool-%d-%d", index+1, toolIndex+1), MessageID: messageID,
				Sequence: toolIndex + 1, Name: call.Name, Kind: call.Kind, Status: call.Status,
				Input: call.Input, Output: call.Output,
			})
		}
	}
	return detail
}

func firstPrompt(messages []store.NewMessage) string {
	for _, message := range messages {
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" {
			return strings.TrimSpace(message.Content)
		}
	}
	return ""
}

func openCodeModel(value string) (string, string) {
	var raw struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	}
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return "", value
	}
	return raw.ProviderID, raw.ID
}
