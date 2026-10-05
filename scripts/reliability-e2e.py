#!/usr/bin/env python3
"""Exercise real CLI imports and HTTP native stores using disposable fixtures.

Failure modes are defined before the implementation changes: a replacement
insert fails after deletion; search rows diverge from messages; forced imports
stop being additive; generic duplicates stop being skipped; Claude deletion
replaces a journal inode while a native writer keeps appending; deletion leaves
transcripts/artifacts visible; detail reads scan an unrelated blocked transcript;
cold detail reads lose parent/child links; unrelated malformed OpenCode summary
rows break the selected detail; cached relationships remain after deletion.
No production store, external provider, or live agent is contacted.
"""

import argparse
import base64
import json
import os
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--artifacts", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    root = args.artifacts or Path(tempfile.mkdtemp(prefix="closeview-reliability-"))
    root.mkdir(parents=True, exist_ok=True)
    root.chmod(0o700)
    checks = []

    def check(name, condition):
        checks.append({"name": name, "passed": bool(condition)})
        print(("PASS " if condition else "FAIL ") + name, flush=True)

    source = root / "opencode.db"
    archive = root / "archive.db"
    with sqlite3.connect(source) as db:
        db.executescript("""
            CREATE TABLE session (id TEXT PRIMARY KEY, parent_id TEXT, title TEXT,
                directory TEXT, agent TEXT, model TEXT, time_created INTEGER, time_updated INTEGER);
            CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, data TEXT, time_created INTEGER);
            CREATE TABLE part (id TEXT PRIMARY KEY, session_id TEXT, message_id TEXT, data TEXT, time_created INTEGER);
            CREATE INDEX message_session ON message(session_id);
            CREATE INDEX session_parent ON session(parent_id);
            INSERT INTO session VALUES ('parent','','Fixture parent','/fixture','','',1700000000000,1700000001000);
            INSERT INTO message VALUES ('msg','parent','{"role":"user"}',1700000000000);
            INSERT INTO part VALUES ('part','parent','msg','{"type":"text","text":"old archive text"}',1700000000000);
        """)

    def import_source(path=source, extra=()):
        result = subprocess.run([str(binary), "import", "--db", str(archive),
                                 *extra, str(path)], text=True, capture_output=True, timeout=20)
        with (root / "imports.log").open("a") as log:
            log.write(result.stdout + result.stderr)
        return result

    check("initial CLI import succeeds", import_source().returncode == 0)
    with sqlite3.connect(archive) as db:
        original_id = db.execute("SELECT id FROM sessions").fetchone()[0]
        db.execute("CREATE TRIGGER reject_replacement BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'fixture insert failure'); END")
    with sqlite3.connect(source) as db:
        db.execute("UPDATE part SET data=? WHERE id='part'", (json.dumps({"type": "text", "text": "replacement archive text"}),))
    check("injected replacement insertion fails", import_source().returncode != 0)
    with sqlite3.connect(archive) as db:
        rows = db.execute("SELECT s.id,m.content FROM sessions s JOIN messages m ON m.session_id=s.id").fetchall()
        search = db.execute("SELECT session_id FROM messages_fts WHERE messages_fts MATCH 'old'").fetchall()
        check("failed replacement preserves original messages", rows == [(original_id, "old archive text")])
        check("failed replacement preserves original search index", search == [(original_id,)])
        db.execute("DROP TRIGGER reject_replacement")
    check("replacement succeeds after storage recovers", import_source().returncode == 0)
    with sqlite3.connect(archive) as db:
        check("successful replacement leaves one refreshed archive", db.execute("SELECT count(*) FROM sessions").fetchone()[0] == 1 and
              db.execute("SELECT content FROM messages").fetchone()[0] == "replacement archive text")
        check("replacement refreshes search atomically", db.execute("SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'old'").fetchone()[0] == 0 and
              db.execute("SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'replacement'").fetchone()[0] == 1)
    check("forced import succeeds", import_source(extra=("--force",)).returncode == 0)
    with sqlite3.connect(archive) as db:
        check("forced import remains additive", db.execute("SELECT count(*) FROM sessions").fetchone()[0] == 2)
    generic = root / "generic.json"
    generic.write_text(json.dumps({"messages": [{"role": "user", "content": "generic fixture"}]}))
    first = import_source(generic, ("--source", "generic"))
    second = import_source(generic, ("--source", "generic"))
    check("generic duplicates remain skipped", first.returncode == second.returncode == 0 and "skipped 1" in second.stdout)

    claude = root / "claude"
    project = claude / "projects" / "fixture"
    project.mkdir(parents=True)
    parent_path = project / "parent.jsonl"
    child_path = project / "parent" / "subagents" / "agent-child.jsonl"
    child_path.parent.mkdir(parents=True)

    def transcript(path, thread, text, agent=None):
        envelope = {"type": "user", "uuid": "fixture-" + thread, "sessionId": thread,
                    "timestamp": "2026-10-05T00:00:00Z", "cwd": "/fixture",
                    "message": {"role": "user", "content": text}}
        if agent:
            envelope.update(agentId=agent, isSidechain=True)
        path.write_text(json.dumps(envelope) + "\n")

    transcript(parent_path, "parent", "Parent fixture")
    transcript(child_path, "parent", "Child fixture", "child")
    history = claude / "history.jsonl"
    history.write_text(json.dumps({"sessionId": "parent", "display": "retained metadata"}) + "\n")
    original_inode = history.stat().st_ino
    with sqlite3.connect(source) as db:
        db.execute("INSERT INTO session VALUES ('child','parent','Fixture child','/fixture','','',1700000000000,1700000001000)")
        # A full catalog scan cannot decode this unrelated row's timestamp.
        db.execute("INSERT INTO session VALUES ('unrelated','','Unrelated','/fixture','','',1700000000000,'invalid timestamp')")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    codex_home = root / "codex"
    codex_home.mkdir()
    codex_fixture = root / "codex-fixture"
    codex_fixture.write_text("#!" + sys.executable + "\n" + '''
import json, os, pathlib, sys, threading, time
home = pathlib.Path(os.environ["CODEX_HOME"])
output_lock = threading.Lock()

def respond(request, result):
    with output_lock:
        print(json.dumps(dict(jsonrpc="2.0", id=request["id"], result=result)), flush=True)

def delayed_list(request, result):
    (home / "list-waiting").touch()
    while (home / "hold-list").exists():
        time.sleep(.005)
    respond(request, result)

threads = [dict(id="codex-parent", name="Parent", createdAt=1, updatedAt=2),
           dict(id="codex-child", name="Child", parentThreadId="codex-parent", createdAt=1, updatedAt=2)]
for line in sys.stdin:
    request = json.loads(line)
    if "id" not in request:
        continue
    method = request["method"]
    with (home / "calls.jsonl").open("a") as log:
        log.write(json.dumps({"method": method}) + "\\n")
    result = {}
    if method == "thread/list":
        result = dict(data=threads, nextCursor=None)
        if (home / "hold-list").exists():
            threading.Thread(target=delayed_list, args=(request, result), daemon=True).start()
            continue
    elif method == "thread/read":
        result = dict(thread=next(row for row in threads if row["id"] == request["params"]["threadId"]))
    elif method in ("thread/items/list", "thread/turns/list"):
        result = dict(data=[], nextCursor=None)
    elif method == "thread/delete":
        threads = [row for row in threads if row["id"] != request["params"]["threadId"]]
    respond(request, result)
''')
    codex_fixture.chmod(0o700)
    env = {**os.environ, "CLOSEVIEW_CLAUDE_HOME": str(claude),
           "CLOSEVIEW_CODEX_HOME": str(codex_home), "CLOSEVIEW_CODEX_BIN": str(codex_fixture),
           "CLOSEVIEW_OPENCODE_DB": str(source), "CLOSEVIEW_OPENCODE_CONFIG_HOME": str(root / "opencode-config"),
           "CLOSEVIEW_CLAUDE_CONFIG_FILE": str(root / "claude-config.json")}
    base = f"http://127.0.0.1:{port}"

    def native_id(source_name, native):
        return source_name + "." + base64.urlsafe_b64encode(native.encode()).decode().rstrip("=")

    def call(path, method="GET", timeout=4):
        request = urllib.request.Request(base + path, method=method,
                                        headers={"X-CloseView-Confirm": "delete-session"})
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                return response.status, json.loads(response.read() or "null")
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read())

    claude_parent = native_id("claude", "fixture/parent.jsonl")
    claude_child = native_id("claude", "fixture/parent/subagents/agent-child.jsonl")
    opencode_parent = native_id("opencode", "parent")
    opencode_child = native_id("opencode", "child")
    with (root / "server.log").open("wb") as log:
        process = subprocess.Popen([str(binary), "serve", "--host", "127.0.0.1", "--port", str(port)],
                                   env=env, cwd=root, stdout=log, stderr=log)
        try:
            for _ in range(100):
                try:
                    with urllib.request.urlopen(base + "/", timeout=.2) as response:
                        assert response.status == 200
                        break
                except (OSError, ValueError):
                    pass
                time.sleep(.05)
            _, detail = call("/api/sessions/" + opencode_parent)
            check("cold OpenCode detail preserves direct child count", detail["session"]["childCount"] == 1)
            _, detail = call("/api/sessions/" + opencode_child)
            check("cold OpenCode detail ignores unrelated malformed summary", detail["session"].get("parentId") == opencode_parent)
            _, detail = call("/api/sessions/" + claude_parent)
            check("cold Claude detail preserves direct child count", detail["session"]["childCount"] == 1)
            _, detail = call("/api/sessions/" + claude_child)
            check("cold Claude detail preserves parent link", detail["session"].get("parentId") == claude_parent)

            codex_parent = native_id("codex", "codex-parent")
            codex_child = native_id("codex", "codex-child")

            def codex_list_count():
                return sum(json.loads(line)["method"] == "thread/list"
                           for line in (codex_home / "calls.jsonl").read_text().splitlines())

            _, detail = call("/api/sessions/" + codex_child)
            check("cold Codex detail preserves parent without catalog scan", detail["session"].get("parentId") == codex_parent and codex_list_count() == 0)
            call("/api/sessions?source=codex")
            before = codex_list_count()
            _, detail = call("/api/sessions/" + codex_parent)
            check("Codex detail reuses catalog child count without relisting", detail["session"]["childCount"] == 1 and codex_list_count() == before)
            (codex_home / "hold-list").touch()
            catalog_errors = []

            def delayed_catalog():
                try:
                    call("/api/sessions?source=codex")
                except Exception as error:
                    catalog_errors.append(str(error))

            pending_catalog = threading.Thread(target=delayed_catalog)
            pending_catalog.start()
            for _ in range(200):
                if (codex_home / "list-waiting").exists():
                    break
                time.sleep(.005)
            status, _ = call("/api/sessions/" + codex_child, "DELETE")
            (codex_home / "hold-list").unlink()
            pending_catalog.join(timeout=5)
            _, detail = call("/api/sessions/" + codex_parent)
            check("Codex deletion invalidates even an in-flight catalog snapshot", status == 204 and detail["session"]["childCount"] == 0 and not catalog_errors and codex_list_count() == before + 1)

            fifo = project / "unrelated.jsonl"
            os.mkfifo(fifo)
            try:
                try:
                    status, detail = call("/api/sessions/" + claude_parent, timeout=.8)
                    bounded = status == 200 and detail["messages"][0]["content"] == "Parent fixture"
                except (TimeoutError, OSError):
                    bounded = False
                check("selected detail never opens unrelated transcript", bounded)
            finally:
                # Release a baseline server blocked in an unwanted catalog read.
                try:
                    writer = os.open(fifo, os.O_WRONLY | os.O_NONBLOCK)
                    os.write(writer, b"\n")
                    os.close(writer)
                except OSError:
                    pass
                fifo.unlink()

            with sqlite3.connect(source) as db:
                db.execute("DELETE FROM session WHERE id='unrelated'")
            call("/api/sessions?source=claude")
            count = 100
            ready = threading.Event()

            def append_native_history():
                with history.open("a") as writer:
                    ready.set()
                    for number in range(count):
                        writer.write(json.dumps({"sessionId": "unrelated", "sequence": number}) + "\n")
                        writer.flush()
                        time.sleep(.001)

            appender = threading.Thread(target=append_native_history)
            appender.start()
            ready.wait()
            status, _ = call("/api/sessions/" + claude_parent, "DELETE")
            appender.join(timeout=5)
            entries = [json.loads(line) for line in history.read_text().splitlines()]
            check("Claude deletion succeeds and removes transcript/artifacts", status == 204 and not parent_path.exists() and not child_path.parent.parent.exists())
            check("Claude deletion preserves shared history inode", history.stat().st_ino == original_inode)
            check("concurrent native history appends all survive", [row["sequence"] for row in entries if "sequence" in row] == list(range(count)))
            check("retained Claude history metadata remains explicit", entries[0].get("sessionId") == "parent")
            _, catalog = call("/api/sessions?source=claude")
            check("deleted Claude transcript absent from catalog", not catalog["sessions"])
            check("deleted Claude detail returns 404", call("/api/sessions/" + claude_parent)[0] == 404)
            call("/api/sessions?source=opencode")
            status, _ = call("/api/sessions/" + opencode_child, "DELETE")
            _, detail = call("/api/sessions/" + opencode_parent)
            check("native deletion clears cached child relationship", status == 204 and detail["session"]["childCount"] == 0)
        finally:
            process.terminate()
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()

    (root / "result.json").write_text(json.dumps({"status": "passed" if all(row["passed"] for row in checks) else "failed", "checks": checks}, indent=2) + "\n")
    print(root)
    if not all(row["passed"] for row in checks):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
