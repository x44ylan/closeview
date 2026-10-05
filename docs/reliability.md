# Storage and native detail behavior

OpenCode archive reimports replace the session, messages, related records, and
search rows in one SQLite transaction. If insertion fails, the original archive
remains readable and searchable. Forced imports remain additive; identical
generic imports remain skipped.

Claude deletion removes the transcript and its session artifact directory. It
preserves entries in Claude's shared `history.jsonl` journal. Native Claude
processes append without participating in CloseView locks, so replacing that
journal can lose concurrent writes. Retained entries can include prompt previews
and session metadata. This deletion is therefore not a complete history erasure.
The viewer lists transcript files, so retained journal entries do not resurrect
deleted conversations.

Opening one conversation reads its own content and local relationship metadata.
OpenCode uses a query restricted to the selected session. Claude checks only
that transcript's parent path and subagent directory. Codex parent links come
from `thread/read`; child counts use the last explicit catalog read. A direct
Codex detail request before catalog loading, or after deletion invalidates that
snapshot, reports zero children until the next catalog refresh. It does not
paginate every Codex thread to determine child counts.

## Isolated end-to-end verification

Build with the installed toolchain supported by the project, then run:

```sh
go build -o /tmp/closeview ./cmd/closeview
python3 scripts/reliability-e2e.py --binary /tmp/closeview --artifacts /tmp/closeview-reliability
```

Use a fresh artifact directory. The runner creates disposable OpenCode and
Claude stores, a synthetic Codex app-server, a legacy archive, and a loopback
HTTP server. It checks failed replacement rollback, search consistency, import
compatibility, native relationships, bounded detail reads, deletion, and
concurrent journal appends. `result.json`, `imports.log`, and `server.log` retain
repeatable evidence. No live provider, native agent store, or deployment changes.
