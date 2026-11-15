# Backend design

The server behind [CONTRACT.md](../CONTRACT.md): one Go binary, one SQLite file.

## Layout

| Path | What |
|---|---|
| `cmd/agent-tasks` | CLI: `serve` (default), `export`, `import`, `version`; flags and env |
| `internal/store` | schema (embedded migrations), all domain operations, search, retention, session context; `places.go` (places, locations, task places), `todos.go`, `session.go` (remote normalisation, hook data) |
| `internal/store/migrations` | SQL migrations, applied in name order, recorded in `schema_migrations` |
| `internal/textproc` | tokenizer, Snowball stemming (Russian / English), FTS query, snippets |
| `internal/plan` | task-list parsing, item matching across revisions |
| `internal/anchor` | comment anchors: resolve a quote, relocate it in a new revision |
| `internal/diff` | Myers line diff, hunks, unified format |
| `internal/mcpserver` | MCP tools (go-sdk), agent-facing descriptions and outputs |
| `internal/api` | JSON API for the web UI under `/api` |
| `internal/server` | HTTP wiring, bearer auth, proxy-header check, session hook, retention loop |
| `internal/webui` | serves the SPA from `internal/webui/dist` (embedded) or `-web-dir` |
| `internal/exportmd` | task → markdown folder, markdown file → revision |
| `Dockerfile` | node (web/ → web/dist) → go build (UI embedded) → alpine runtime |

Dependencies: `modernc.org/sqlite` (pure Go, FTS5 included), `github.com/modelcontextprotocol/go-sdk`,
`github.com/kljensen/snowball`. Exact versions in `go.mod`.

## HTTP

| Path | Auth | What |
|---|---|---|
| `/mcp` | bearer token | MCP Streamable HTTP, stateless mode |
| `/hooks/session-context?repo=&cwd=&machine=` | bearer token | registers the location; text/plain lines (tasks, open todos, other places' paths) for the SessionStart hook; empty when nothing matches |
| `/api/...` | proxy header (optional) | JSON API, all writes as `owner` |
| `/healthz` | none | liveness |
| everything else | proxy header (optional) | web UI; unknown paths get `index.html` |

The web UI and `/api` trust the reverse proxy. With `-web-auth-header` set (e.g.
`Cf-Access-Authenticated-User-Email`), the server refuses requests without that header, so the
UI stays closed to anyone who reaches the port directly.

### JSON API

Request bodies are JSON. Unknown fields get a 400, so an outdated client fails visibly.
Repeated filters accept both `tag=a&tag=b` and `tag=a,b`.

```
GET    /api/config                                   {base_url, workspace_root, version, statuses, doc_kinds, journal_kinds,
                                                      close_reasons: [done, cancelled], todo_states, machines: [name, ...] (most
                                                      recently seen first), user? (when -web-auth-header is set)}
GET    /api/tags?q=&limit=                           {tags: [{tag, count, tasks, todos}]}: distinct tags of tasks and todos,
                                                      count = tasks + todos carrying it. q: case-insensitive; tags starting
                                                      with q first, then containing q; within each group by count desc, tag.
                                                      Without q: all tags by count. limit default 20, max 200
GET    /api/tasks?q=&status=a,b&tag=x&tag=y&place=<key>|none&sort=updated|created|relevance&limit=&offset=
                                                     {total, tasks: [task + {url, workspace, hits: [hit], plan_progress: {done,total}|null,
                                                      doc_count, submitted_rounds, draft_rounds}],
                                                      todos: [todo + {kind: "todo", snippet}]  (with q: up to 10 matching todos,
                                                                                                any state, not filtered by task filters),
                                                      status_counts: {active, waiting, later, archived}  (all filters except status),
                                                      tag_counts: [{tag, count}]                        (whole result, all filters)}
POST   /api/tasks/bulk                               {ids: ["T-1", ...], status, closed_reason? (with archived), waiting_on?}
                                                     → {tasks: [task]}; one transaction: all change or none (error names the task)
POST   /api/tasks                                    {title, summary?, tags?, status?, waiting_on?,
                                                      places?: [{place, role?: primary|touched, note?}],
                                                      from_todos?: ["D-1", ...]}   → 201 task   ("create task from these todos")
GET    /api/tasks/{id}?machine=                      task + documents + plan_progress + journal(50) + pending_rounds + draft_rounds
                                                     + todos (in_task). places[].locations: every machine, always (v0.6); with
                                                     ?machine=, places[].here = the path on that machine
PATCH  /api/tasks/{id}                               {title?, summary?, status?, waiting_on?, closed_reason?: done|cancelled,
                                                      tags_add?, tags_remove?, places_add?: [{place, role?, note?}], places_remove?: [key]}
GET    /api/places?q=                                {places: [{key, kind, label, created_at, locations[{machine, path, seen_at}],
                                                      tasks, open_todos}]}  q: case-insensitive substring of label (listed
                                                      first), key or a location path; empty q = all places
GET    /api/place?key=<key>                          one place with locations (404 if unknown)
GET    /api/todos?place=<key>|none&state=open,done,dropped,in_task&tag=x&q=&limit=&offset=
                                                     {total, todos: [todo], state_counts: {open, done, dropped, in_task}
                                                      (all filters except state), tag_counts: [{tag, count}] (whole result)}
                                                     default state: open; default limit 50, max 200
POST   /api/todos                                    {text, note?, places?: [key], origin_task?, tags?}  → 201 todo (source "owner")
GET    /api/todos/{id}                               todo
PATCH  /api/todos/{id}                               {text?, note?, state?: open|done|dropped, places_add?, places_remove?,
                                                      tags_add?, tags_remove?}  → todo
POST   /api/todos/bulk                               {ids: ["D-1", ...], state: open|done|dropped} → {todos: [todo]}; all or none
GET    /api/tasks/{id}/journal?kind=&limit=
POST   /api/tasks/{id}/journal                       {kind, body, source?}
GET    /api/tasks/{id}/docs/{slug}?rev=n             document, revision (body), items, comments (all rounds, drafts too), revisions, rounds
PUT    /api/tasks/{id}/docs/{slug}                   {body, kind?, title?, message?, base_revision?}  → write result (as doc_write:
                                                      document, revision, created, unchanged, items, reanchored, outdated); author
                                                      owner; plan items matched and comments re-anchored as for any revision;
                                                      409 when base_revision is given and the document has a newer revision
POST   /api/tasks/{id}/docs/{slug}/revert            {to, base_revision?}  → write result as PUT: a new revision with the body of
                                                      r<to>, author owner, message "revert to r<to>", usual item matching and
                                                      re-anchoring; 409 on stale base_revision; unchanged: true (no new revision)
                                                      when r<to> equals the latest; 404 if r<to> does not exist
POST   /api/tasks/{id}/docs/{slug}/approve           {revision, submit_draft?}  → {document, approval, round?}: the owner approves
                                                      the latest revision (409 if `revision` is not the latest); submit_draft
                                                      first submits a draft round that has comments ("approve with comments");
                                                      approving again changes nothing; a decision entry goes to the journal
DELETE /api/tasks/{id}/docs/{slug}/approve           → {document}: withdraw the approval of the latest revision (409 if none)
GET    /api/events                                   Server-Sent Events: `event: change` (data {}) after every committed change,
                                                      whoever made it; a `: ping` comment every 25 s; the page re-reads what it shows
GET    /api/tasks/{id}/docs/{slug}/revisions
GET    /api/tasks/{id}/docs/{slug}/diff?from=&to=    {unified, hunks[{old_start, old_lines, new_start, new_lines, lines[{op, old_n, new_n, text}]}]}
POST   /api/tasks/{id}/docs/{slug}/items/{item}      {done?, note?}  → item; the owner's tick/note: done_by and notes[].by = "owner"
GET    /api/tasks/{id}/docs/{slug}/rounds
POST   /api/tasks/{id}/docs/{slug}/rounds/draft      {revision?} → the document's draft round (created if missing)
GET    /api/rounds/pending?task=
GET    /api/rounds/{round}
PATCH  /api/rounds/{round}                           {summary}            (draft only)
DELETE /api/rounds/{round}                                                (draft only)
POST   /api/rounds/{round}/submit
POST   /api/rounds/{round}/comments                  {body, anchor?: {quote, start?, end?}} (draft only)
PATCH  /api/comments/{id}                            {body} (draft) | {state: resolved|open}
DELETE /api/comments/{id}                                                 (draft only)
POST   /api/comments/{id}/replies                    {body, resolve?, reopen?}
```

Errors are `{"error": "..."}` with 400 (bad input), 404, 409 (state conflict, e.g. editing a
submitted round, stale `base_revision`).

A search **hit** (v0.6) is `{kind, source, url, text, highlights, slug?, revision?, start?, end?,
line?, comment?, round?, journal?}`:

| kind | source | position fields | url |
|---|---|---|---|
| `task` | `task` | — | `/t/<id>` |
| `document` | `doc:<slug>` | `slug`, `revision` (latest), `start`/`end` (code points in the body), `line` | `/t/<id>/d/<slug>/r/<n>?at=<start>-<end>` (no `?at` if only the title matched) |
| `comment` | `comment:<id>` | `slug`, `revision` (reviewed), `round`, `comment` | `/t/<id>/d/<slug>/r/<n>?c=<comment>` |
| `round` | `round:<id>` (summary) | `slug`, `revision` (reviewed), `round` | `/t/<id>/d/<slug>/r/<n>?round=<round>` |
| `journal` | `journal:<id>` | `journal` | `/t/<id>?j=<entry>` |

`text` and `highlights` (rune ranges within `text`) are the snippet. Up to 3 hits per task, best
first. Todo hits are listed separately in `todos` with `kind: "todo"`, `url: /todos/<id>` and a
`snippet: {text, highlights}`. The UI handles `?at=`, `?round=` and `?j=` (scroll to and
highlight); MCP `task_search` returns the same `kind` and `url`.

A comment has `revision` (reviewed), `anchor` (on that revision), `line`, `current` (`{revision,
quote, start, end, line}` — where it sits in the newest revision it was found in), `outdated_in`
(revision where its text disappeared). The UI shows a comment on revision R when
`revision == R` or `current.revision == R`.

"Create task from these" on the todo page is `POST /api/tasks` with `from_todos`: the todos go
`in_task` and their places join the task as `touched`. A todo is `{id: "D-n", text, note, state,
task?, origin_task?, places[{key, kind, label}], tags, source, created_at, updated_at,
closed_at?, url}`; its web url is `/todos/<id>`, the list `/todos?place=<key>`.

A plan item is `{id: "i3", text, done, done_at?, done_by?: owner|agent (who ticked or unticked
last; a new "- [x]" line counts as written by the revision's author), line, notes: [{by:
owner|agent, body, created_at}]}`. **Changed in v0.5:** notes carried `author`, now `by`.

A round has `addressed_revision` (latest revision n when `round_addressed` was called) for "answered in
rN". MCP comment results carry `url` = `/t/<id>/d/<slug>/r/<n>?c=<comment id>`; the UI scrolls
to `?c=`.

## Schema

Two migrations: `0001_init.sql` and `0002_places_todos.sql`. `0001` was edited in place up to
v0.6 because no database was deployed yet. From the first release on, migrations only get added.

- `tasks` — `id INTEGER PRIMARY KEY AUTOINCREMENT` (shown as `T-<id>`, never reused), title,
  summary, status, waiting_on, closed_reason (`done|cancelled`), archived_at, created/updated.
- `places` — kind (`git|path`), unique key, label. `place_locations (place, machine, path,
  seen_at)` — learned, several paths per machine allowed. `task_places (task, place, role, note)`.
- `todos` — `AUTOINCREMENT` id (`D-<id>`), text, note, state, `origin_task_id` and `task_id`
  (both `ON DELETE SET NULL`: deleting a task never deletes a todo), source, timestamps.
  `todo_tags`, `todo_places`, FTS5 `todo_search (todo_id, raw, stems)`.
- `task_tags (task_id, tag)`.
- `documents` — task, slug (unique per task), kind, title, `next_item` counter for item ids.
- `revisions` — document, n, body, author, message, created_at. Never updated.
- `plan_items` — stable per-document key (`i1`…), done, done_at, done_by. `plan_item_revisions`
  records where an item appears in each revision (position, line, text): the current view is
  the latest revision's rows, history stays. `plan_item_notes` — author (`by`), body, time.
- `review_rounds` — document, n, reviewed revision, state, summary, agent message, timestamps,
  addressed_revision.
- `comments` — round, body, state, original anchor (`quote, prefix, suffix, start_off,
  end_off`), current anchor (`cur_revision_id, cur_*`), `outdated_revision_id`.
  `comment_replies` — author, body, time.
- `journal` — task, kind, body, source, time.
- `search` — FTS5 table `(task_id, src, raw, stems)`; only `stems` is indexed.

All timestamps are fixed-width UTC strings (`2006-01-02T15:04:05.000000Z`), so they sort as text.
Foreign keys cascade: deleting a task removes everything it owns; its search rows are deleted
explicitly. One connection, one transaction at a time (WAL mode); fine for a single owner.

## Algorithms

**Search.** `textproc` splits text into letter/digit runs, lower-cases, folds `ё`→`е`, and
stems each word: Snowball Russian for Cyrillic, Snowball English for Latin, as is otherwise.
The FTS column holds the stems joined by spaces (`unicode61` then only splits on spaces). A query
is stemmed the same way and becomes `"stem1"* AND "stem2"*`. Indexed sources: task title +
summary (`task`), the latest revision of each document (`doc:<slug>`), journal entries
(`journal:<id>`), comments with replies once their round is submitted (`comment:<id>`), round
summaries (`round:<id>`). Tasks are ranked by their best `bm25`. Snippets are cut from the raw
text around the first word whose stem starts with a query stem.

**Plan items.** Every `- [ ] text` / `* [x] text` / `1. [ ] text` line outside fenced code is an
item. On a new revision, items are matched against the previous revision's items:
1. identical text (case and whitespace normalised), nearest position first;
2. remaining pairs with similarity ≥ 0.6 (1 − Levenshtein / max length) whose positions differ
   by at most max(3, n/4), best similarity first.
Matched items keep id, done and notes; unmatched new items get the next id and the box state
written in the body; vanished items keep their rows (history) but are not in the current view.
Ticks and notes never create revisions.

**Re-anchoring.** On every new revision, each anchored comment that is open or resolved is
relocated from its current revision to the new one:
1. exact quote — every occurrence is scored by how much of the 32-rune prefix/suffix agrees and
   by distance from the expected position (old offset scaled by the length change);
2. fuzzy — for quotes of 8+ runes, approximate substring search (Sellers) within ±3000 runes of
   the expected position; accepted at edit distance ≤ 25% of the quote, ties go to the length
   closest to the quote;
3. otherwise an open comment becomes `outdated` (`outdated_in` = the new revision); it stays
   anchored to its original revision. Resolved comments keep their state.
When a draft round made on an older revision is submitted, its comments are carried to the
latest revision the same way. New comments: the server takes `[start, end)` if it holds the
quote, else the occurrence nearest to `start`, and computes prefix/suffix itself. Offsets are
Unicode code points of the markdown source.

**Diff.** Myers O(ND) on lines after stripping the common head and tail; above 1500 edits the
changed middle is shown as removed + added (bounded memory). Hunks with 3 lines of context.

**Retention.** At start and then every 24 h, archived tasks whose `archived_at` is older than
`retention-days` are deleted with everything they own. `0` turns this off. Todos survive: their
`origin_task` and `task` just become empty.

**Places.** A git remote normalises to `host/path`: scheme, credentials (`user:pw@`), port and
`.git` dropped, `git@host:path` turned into `host/path`, host lower-cased. A place reference is
a path key when it looks like `<machine>:/abs/path` (no `@` before the colon), otherwise it is
normalised as a remote. Resolving "here" (`place_here`, the session hook):
1. with a remote → the git place of that remote (created if new); the location is recorded
   for the machine, except that a `cwd` inside an already known location of that place on that
   machine just refreshes it (so subfolders don't pile up as locations);
2. without a remote → the place with the longest known location on this machine that contains
   `cwd` (path-boundary match);
3. otherwise a new path place `<machine>:<cwd>`, located at `cwd`.
Unknown places named in `places`/`places_add` are created without locations.

**Task from todos.** `task_create from_todos` takes only open todos: they become `in_task` with
`task` set, their places join as `touched`, and one journal entry lists them. Reopening an
`in_task` todo clears its task. Task merging (`merged_into`, `from_tasks`, `closed_reason:
merged`) was removed in v0.5.

**Facets.** `GET /api/tasks` and `GET /api/todos` add counts for the filter controls: per
status/state with every filter except status/state applied, and per tag over the whole current
result. Two extra grouped queries per list request; fine at single-owner scale.

**Session context.** Resolve the place as above (registering the location), then list its
non-archived tasks (active, waiting, later; at most 20) plus the task named by a `T-<n>`
component of `cwd` (a workspace folder), each with pending rounds and its other places with the
path on this machine ("not on this machine" when unknown), and up to 20 open todos of the place.
The machine name comes from the client (`AGENT_TASKS_MACHINE`, else the host name); the server
never guesses it.

**review_wait.** SQLite is not polled. After `SubmitRound` commits, it signals an in-process
notifier: a channel that is closed and replaced on every signal. A waiter:
1. takes the current channel;
2. reads the pending rounds for its task or document and returns them if there are any;
3. otherwise blocks until the channel closes (then goes back to step 1), the timeout passes
   (returns `{waiting: true}`), or the caller's context ends (the client cancels, sends
   `notifications/cancelled`, or drops the HTTP request; `PropagateRequestCancellation` is on).

Because the channel is taken before the read, a submit that lands between the read and the
wait still wakes the waiter. `timeout_s` defaults to 300 and is capped by `-review-wait-max`
(900). If the request carries a progress token, the server sends a progress notification every
20 s; this starts the SSE response and keeps idle timers from firing. Only this process submits
rounds, so the in-process notifier is enough. A second server on the same file would not wake
the first one's waiters.

### MCP client timeouts for review_wait

Checked in the vendors' docs on 2026-11-15:

- **Claude Code** — `MCP_TOOL_TIMEOUT` (ms) is the wall-clock limit per tool call, about 28 hours
  when unset; a per-server `"timeout"` (ms) in the `.mcp.json` entry overrides it. HTTP servers
  also have an idle limit: a call that receives no response and no progress notification for 5
  minutes is aborted (`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`, ms); a per-server `timeout` ≥ 1000
  raises that floor. Calls running over 2 minutes move to a background task
  (`CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS`). Recommended entry:
  ```json
  { "mcpServers": { "agent-tasks": { "type": "http", "url": "https://tasks.example/mcp",
      "headers": { "Authorization": "Bearer ${AGENT_TASKS_TOKEN}" }, "timeout": 960000 } } }
  ```
- **Codex CLI** — `tool_timeout_sec` per server, default **60 s**; `startup_timeout_sec`
  default 10 s. Without a change every `review_wait` longer than a minute fails. Recommended:
  ```toml
  [mcp_servers.agent-tasks]
  url = "https://tasks.example/mcp"
  bearer_token_env_var = "AGENT_TASKS_TOKEN"
  tool_timeout_sec = 960
  ```
- **Reverse proxies** — Cloudflare answers 524 when the origin sends nothing for 100 s. A
  `review_wait` response starts only with its first message. Behind Cloudflare, either the
  client sends a progress token (the server then sends something every 20 s), or the wait stays
  under about 90 s (`-review-wait-max 90`, see Deployment). Not yet tested through a live
  Cloudflare tunnel.

## Config

| Flag | Env | Default |
|---|---|---|
| `-listen` | `AGENT_TASKS_LISTEN` | `:8080` |
| `-db` | `AGENT_TASKS_DB` | `agent-tasks.db` (image: `/data/agent-tasks.db`) |
| `-base-url` | `AGENT_TASKS_BASE_URL` | `http://localhost:8080` |
| — | `AGENT_TASKS_TOKENS` | MCP/hook tokens, `name=secret` separated by commas |
| `-tokens-file` | `AGENT_TASKS_TOKENS_FILE` | file with one `name=secret` per line, `#` comments |
| `-retention-days` | `AGENT_TASKS_RETENTION_DAYS` | `90` |
| `-review-wait-max` | `AGENT_TASKS_REVIEW_WAIT_MAX` | `900` (seconds; cap of `review_wait` `timeout_s`) |
| `-workspace-root` | `AGENT_TASKS_WORKSPACE_ROOT` | `~/agent-work` (returned as is, not expanded) |
| `-web-auth-header` | `AGENT_TASKS_WEB_AUTH_HEADER` | empty (no check) |
| `-web-dir` | `AGENT_TASKS_WEB_DIR` | empty (embedded UI) |
| `-mcp-no-auth` | `AGENT_TASKS_MCP_NO_AUTH` | `false`; development only |

Tokens are never accepted as flags (they would show in `ps`) and never logged; logs show token
names. Without tokens `/mcp` and `/hooks` answer 503.

## Deployment

The image is `ghcr.io/svesh87/agent-tasks:<YYYY.MM.DD-run>` (or `:sha-<short>`); release binaries
for linux/darwin amd64/arm64 are attached to GitHub releases (`v*` tags). One process, one
SQLite file: keep `/data` on a persistent volume and run a single instance (the `review_wait`
notifier and the write lock are in-process).

Typical setup behind an auth proxy, e.g. Cloudflare Access in front of a tunnel:

- **Web UI and `/api`**: an Access application for people; start the server with
  `-web-auth-header Cf-Access-Authenticated-User-Email` so requests that bypass the proxy are
  refused.
- **`/mcp` and `/hooks/*`**: a second Access application (or a path rule) that accepts
  **service tokens** (`CF-Access-Client-Id` / `CF-Access-Client-Secret` headers, which agent
  clients can send). The server's own bearer token is still required: the proxy decides who may
  reach the server, the bearer decides who may use the tools.
- **Idle requests**: proxies drop a request that sends no bytes for a while (Cloudflare: about
  100 s, then a 524). A `review_wait` response starts only with its result unless the client
  asked for progress, so cap it below that: `-review-wait-max 90`
  (`AGENT_TASKS_REVIEW_WAIT_MAX=90`). Agents then call `review_wait` again on `{waiting: true}`.
- **Base URL**: `-base-url https://tasks.example.com` so links in tool results point at the
  proxy hostname.
- **Backups**: the database is in WAL mode; copy it with `sqlite3 agent-tasks.db ".backup
  out.db"` or stop the container and copy `agent-tasks.db*`; `agent-tasks export` gives a
  readable markdown copy of a task.
- **Retention**: archived tasks are deleted after `-retention-days` (90); set `0` to keep them.

Example (tokens from a file mounted read-only and readable by uid 10001, never on the command
line):

```sh
docker run -d --name agent-tasks -p 127.0.0.1:8080:8080 \
  -v agent-tasks-data:/data -v /etc/agent-tasks/tokens:/run/tokens:ro \
  -e AGENT_TASKS_TOKENS_FILE=/run/tokens \
  -e AGENT_TASKS_BASE_URL=https://tasks.example.com \
  -e AGENT_TASKS_WEB_AUTH_HEADER=Cf-Access-Authenticated-User-Email \
  -e AGENT_TASKS_REVIEW_WAIT_MAX=90 \
  ghcr.io/svesh87/agent-tasks:<tag>
```

## CI

`.github/workflows/ci.yml`: gofmt, vet and tests on every pull request and push; on a push to
`main` the image is built and pushed to `ghcr.io/<owner>/agent-tasks` (tags
`YYYY.MM.DD-<run number>` and `sha-<short>`, `GITHUB_TOKEN` with `packages: write`); on a `v*`
tag the web UI is built, binaries for linux/darwin × amd64/arm64 are cross-compiled with it
embedded, and attached to a GitHub release with `SHA256SUMS`. Actions are pinned by commit.

## Development

Go is not needed on the host; everything runs in the pinned image:

```sh
docker run --rm -v "$PWD":/src -w /src -v agent-tasks-gomod:/go/pkg/mod \
  -e CGO_ENABLED=0 golang:1.27.2-alpine3.24 sh -c 'go vet ./... && go test ./...'

docker build -t agent-tasks:local .
export AGENT_TASKS_TOKENS="dev=$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"
docker run --rm -p 127.0.0.1:8080:8080 --tmpfs /data:uid=10001,gid=10001 \
  -e AGENT_TASKS_TOKENS agent-tasks:local     # throwaway DB in tmpfs

docker run --rm -v "$PWD/backup":/out -v agent-tasks-data:/data agent-tasks:local \
  export -task T-12 -out /out
```
