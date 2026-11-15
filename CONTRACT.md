# agent-tasks contract, v0

The shared agreement between the server (this repository) and the agent rules that use it.
The server implements it; skills and rules rely on it. Changes go through this file first,
with a line in the changelog at the end.

## What it is for

**Owner** below means the person the agents work for; they review in the web UI.

A single owner works with coding agents (Claude Code, Codex) on several machines and in an
agent runner. Work often spans several repositories. A task lives here, not in any one
repository: its documents (plan, research, review notes), the owner's review of them, plan
progress and the agent's running context. The owner reads and reviews in the web UI; agents
work through MCP. Old archived tasks are deleted after a retention period.

What the server does not hold: scratch files, scripts, data dumps. Those live in the task's
workspace folder on disk, `<workspace_root>/<task id>/` (server config, default `~/agent-work`),
synced between machines outside this server if the user wants. The server only knows the
convention and returns the path in `task_get`.

The skills shipped in this repository are self-contained: they need only this server's MCP
connection, not any hooks, global rules or other repositories of the author. Anyone who runs
the server and installs the plugin can use them as they are.

No process is imposed: no spec stages, no gates, no required document set. A task may have
no documents at all.

## Model

**Task**
- `id`: `T-<n>`, sequential, never reused.
- `title`, `summary` (short markdown, optional).
- `status`: `active` | `waiting` | `later` | `archived`.
  - `waiting` carries `waiting_on` (free text: "owner review", "Cloudflare support"...).
  - `archived` carries `closed_reason`: `done` | `cancelled`, and `archived_at`.
- `tags`: free strings. A prefix such as `topic:` or `area:` is only a naming habit, not
  special to the server. Filters work on any tag. Repositories and folders are places
  (v0.3), not tags.
- `created_at`, `updated_at`.

**Document** — belongs to a task.
- `slug`, unique within the task (`plan`, `research`, `review-2026-11-20`...).
- `kind`: `plan` | `research` | `review` | `notes` | `other`.
- `title`.
- Content lives only in revisions.

**Revision** — immutable.
- `n`: 1, 2, 3... per document.
- `body`: markdown.
- `author`: `agent` | `owner` | `import`.
- `message`: optional one line ("addresses review round 2").
- `created_at`.

**Plan item** — every markdown task-list line (`- [ ] text`) in a document.
- `id`: stable, server-assigned (`i1`, `i2`...). On a new revision the server matches items to
  the previous revision (exact text, then closest similar text in the same position range);
  matched items keep id, `done` and notes; unmatched ones are new; vanished ones are dropped
  from the current view but kept in history.
- `text` (as written), `done` (bool), `notes` (list of short timestamped entries).
- Ticking and notes do **not** create a revision. Rendering shows the box state from the item,
  not from the `[ ]`/`[x]` written in the body.

**Review round** — the owner's batch of comments on one document.
- `n` per document, tied to the revision that was reviewed.
- `state`: `draft` (owner still writing; invisible to agents) → `submitted` → `addressed`.
- `summary`: optional general remark from the owner.
- Submitting a round is the "review is ready" signal.

**Comment** — in a round.
- `anchor`: either none (general comment) or a text anchor on the reviewed revision:
  `quote` (exact selected text), `prefix`, `suffix` (up to 32 chars each), `start`, `end`
  (offsets in the markdown body).
- `body`: markdown.
- `state`: `open` | `resolved` | `outdated`. `outdated` is set by the server when a new
  revision no longer contains the anchored text (exact quote, then fuzzy match near the old
  position); it stays visible against its original revision.
- `replies`: list of `{author: agent|owner, body, created_at}`.
- The owner may reopen a resolved comment.

**Journal entry** — belongs to a task.
- `kind`: `context` (snapshot of where the work stands, for a later session) | `log` (what was
  done) | `decision` (what was decided and why, incl. the owner's choices).
- `body`: markdown; `created_at`; `source`: free text (machine, session, agent).

## MCP tools

Transport: Streamable HTTP at `/mcp`, `Authorization: Bearer <token>`. Every tool that
returns a task or document includes its web `url`. Errors are plain messages an agent can
act on.

| Tool | Does |
|---|---|
| `task_create(title, summary?, tags?, status?)` | New task, default `active`. Returns task + url. |
| `task_get(id)` | Task, its documents (latest revision n, open comment count), plan progress, last journal entries, pending rounds. |
| `task_update(id, title?, summary?, status?, waiting_on?, closed_reason?, tags_add?, tags_remove?)` | Change fields; archiving needs `closed_reason`. |
| `task_search(query?, status?[], tags?[], sort?, limit?, offset?)` | Full-text over titles, summaries, latest revisions, journal, comments (Russian and English stemming) + filters. Default: non-archived, by `updated_at` desc. Returns snippets. |
| `doc_write(task, slug, body, kind?, title?, message?)` | Create the document or add a revision. Returns revision n, plan items with ids, re-anchoring result (comments that went `outdated`), url. |
| `doc_get(task, slug, revision?)` | Body of a revision (latest by default), plan items with state and notes, open comments with anchors. |
| `doc_diff(task, slug, from, to?)` | Unified diff between revisions. |
| `plan_item_set(task, slug, item, done?, note?)` | Tick/untick an item and/or append a note. |
| `review_pending(task?)` | Submitted, not addressed rounds (all tasks or one), with comments, anchors and the revision they refer to. |
| `comment_reply(comment, body, resolve?)` | Reply; optionally resolve. |
| `round_addressed(round, message?)` | Mark the round handled; usually after `doc_write`. |
| `journal_append(task, kind, body, source?)` | Add a journal entry. |
| `journal_get(task, kind?, limit?)` | Latest entries. |

## Web

Base URL configurable (e.g. `https://tasks.example.com`). Stable links agents put in chat:
- `/t/<id>` — task;
- `/t/<id>/d/<slug>` — document, latest revision, review mode;
- `/t/<id>/d/<slug>/r/<n>` — a given revision;
- `/t/<id>/d/<slug>/diff/<a>..<b>` — diff.

Screens: task list (search, status and tag filters, sort), task page (documents, plan
progress, journal, pending rounds), document review (select text → comment, general comment,
draft round, submit), revision history and diff, outdated comments shown on their revision.
UI languages: English and Russian. Works on a phone.

Access control: the web UI sits behind an auth proxy (Cloudflare Access); the server trusts
it and has no logins of its own. MCP uses bearer tokens from config.

## Session hook endpoint

`GET /hooks/session-context?repo=<host>/<path>&cwd=<path>&machine=<name>` with the same bearer
token as MCP. `repo` is optional: without it the place is the directory itself. Returns
`text/plain`, a few lines meant to be injected into an agent's context at session start and
after context compaction: non-archived tasks of this place (id, status, title, waiting_on,
open review rounds), their workspace paths, their other places with paths on this machine,
and open todos of the place. Empty body when nothing matches. Must answer fast (<300 ms).

The plugin ships a Claude Code `SessionStart` hook (matchers `startup`, `resume`, `compact`).
It takes `repo` from `git remote get-url origin` of the cwd when there is one (normalised to
`host/path`, no `.git`, no credentials), calls this endpoint with a 2 s timeout and prints the
body. Settings come from env (`AGENT_TASKS_URL`, `AGENT_TASKS_TOKEN`, `AGENT_TASKS_MACHINE`)
or `~/.config/agent-tasks/config` (`url`, `token`, `machine`). Extra request headers for an
auth proxy: `header = Name: value` lines in that file, `AGENT_TASKS_HEADERS` (one per line) or
`AGENT_TASKS_HEADERS_FILE`; secrets go to curl through stdin, never on the command line. Any
failure — no config, server down, an error status — prints nothing and exits 0.

## Storage and maintenance

- SQLite, single file; FTS5 index with stemming done in the application (Snowball Russian and
  English).
- Export: a task as a folder of markdown (task.md with front matter, one file per document's
  latest revision, journal.md). Import: a markdown file becomes a new revision (`author:
  import`).
- Retention: archived tasks older than N days (config) are deleted with everything they own,
  by a built-in daily job. The workspace folder on disk is cleaned by the owner's own cron,
  using the same rule.

## Places and todos (v0.3)

The server must not depend on how a particular user lays out repositories on disk. Work is
tied to **places**, identified the same way on any machine for any user.

**Place**
- `kind: git` — a repository, `key` = its normalised `origin` remote (`host/path`: no scheme,
  credentials, port or `.git`; `git@host:path` understood). Same on every machine.
- `kind: path` — a directory without a remote (experiments folder, local-only repo, a task
  folder), `key` = `<machine>:<absolute path>`. Honest "here and only here".
- `label`: short display name (last path component by default).

**Place location** — where a place lives on a machine: `{place, machine, path, seen_at}`.
The server learns locations itself: the session hook and MCP calls that carry
`cwd`/`remote`/`machine` register them. Tools return the path for the caller's machine, or
say the place is not present there. No layout conventions are assumed.

**Task places**: `Task.places[]` = `{place, role: primary|touched, note?}`, editable at any
time (a task may start with none). Filtering and search accept `place`. The `repo:` tag
namespace is no longer the source of truth; free tags remain.

**Todo** — a short note the agent records when asked, to grow into a task later or be
dropped. Not a plan item: plan items are steps inside a task's document; todos stand alone.
- Todos are standalone: as a rule they belong to no task. `origin_task` is only provenance —
  the task where the idea came up, optional — and creates no link of ownership; closing or
  deleting that task does not touch the todo (the reference is just cleared on deletion).
- `id` (`D-<n>`), `text` (one line), `note?` (markdown), `places[]` (zero or more),
  `origin_task?`, `tags[]`, `state: open | done | dropped | in_task`,
  `task?` (set when `in_task`), `created_at`, `source`.

**Merging**: a new task can be formed from todos and from parts of other tasks. Todos used go
`in_task`; a task fully absorbed is archived with `closed_reason: merged` and `merged_into`.

MCP additions and changes:

| Tool | Does |
|---|---|
| `place_here(cwd, remote?, machine)` | Resolve the caller's directory to a place (create if new), register its location, return place + tasks and open todos for it. |
| `place_get(key)` | A place with its known locations per machine. |
| `task_create(..., places?, from_todos?, from_tasks?)` | `from_tasks` = list of `{task, note}` describing what was taken; links go to the journal. |
| `task_update(..., places_add?, places_remove?, merged_into?)` | `closed_reason` gains `merged`. |
| `task_search(..., place?)` | Filter by place key. |
| `todo_add(text, note?, places?, origin_task?, tags?)` | New todo. |
| `todo_list(place?, state?, query?, tags?, limit?)` | Default: open todos. `place` matches todos of that place; `place: none` = todos without places. |
| `todo_update(id, text?, note?, state?, places_add?, places_remove?, tags_add?, tags_remove?)` | Edit or close (`done`, `dropped`). |

The session hook endpoint takes `machine` too, registers the location, and lists tasks and
open todos of the place, with paths of the tasks' other places on this machine.

Web: places on the task page; a todo list page (filters: place, none, state, tags, search),
selecting several todos → "create task from these". Links: `/todos`, `/todos/<id>`,
`/todos?place=<key>`.

Details (v0.3.1):
- Places named in input that the server has not seen are created without locations.
- Agents pass the repository root as `cwd` to `place_here`; a subdirectory of a known place
  on the same machine resolves to that place and adds no location.
- `task_get(machine)` returns place paths for that machine only.
- `merged_into` alone archives the task with `closed_reason: merged`. `task_create` and
  `task_update` take an optional `source` for the journal entries they write.
- The session hook without `machine` finds the place by `repo` but registers no location.

## Approval and live updates (v0.7)

**Approval** — the owner's explicit "go" on a document revision, given in the web UI.
- Belongs to a revision: `{revision, at, by: owner}`. It counts while that revision is the
  document's latest; a new revision (by anyone) leaves the document unapproved. The older
  revision keeps its mark in history.
- The owner may withdraw it until a new revision appears.
- Approving with a non-empty draft round submits the draft along with it ("approve with
  comments"); a submitted, not addressed round does not block approval. The agent answers the
  comments and starts the work unless they ask for a change of plan; a new revision removes
  the approval as always.
- Only the owner approves; agents have no tool for it.
- Each approval and withdrawal adds a `decision` journal entry with `source: web`.

MCP changes:
- `doc_get`: `approved` (approval of the latest revision, or null) and `approved_revisions`
  (revisions approved at some point).
- `task_get`: each document carries `approved`.
- `review_pending(task)`: with `task` given, `approvals` lists the task's documents whose latest
  revision is approved, with links.
- `review_wait(task, doc)`: also returns when the latest revision of that document is approved,
  with `approved` and the revision `url` (rounds pending at the same time come along). Only when
  `doc` is given. An approval is not consumed: waiting again returns it again until a new
  revision is written.
- The session hook endpoint shows `<slug> r<n> approved` on a task's line.

Skills: the owner's "go" on a plan is the approval of its latest revision in the web UI or an
explicit "yes" in the chat.

**Live updates.** An open page shows changes made elsewhere (an agent through MCP, another tab
or device) without a reload. The web UI listens to `GET /api/events` (Server-Sent Events behind
the same auth proxy): `event: change` after every committed change, a keep-alive comment every
25 s; the page re-reads what it shows. Internal to this repository's UI, not used by agents.

## Owner feedback, round 2 (v0.6)

- **Search jumps to the hit.** Every search result says what matched (`kind: task | document |
  comment | round | journal | todo`) with the deep-link `url` of that object (document at the
  revision and position, comment via `?c=`, todo page), not only the task. Same in
  `task_search` (MCP) results.
- **Bulk status changes** from the web: several tasks → active / waiting / later / archived
  (with `closed_reason`); several todos → done / dropped / open (besides "create task from
  these"). REST bulk endpoints; MCP stays per item.
- **The owner edits documents** in the web and saves a revision (`author: owner`); plan items
  are matched as for any revision; open comments are re-anchored.
- **The owner renames a task** and edits its summary in the web.
- **Several machines per task.** A task can move between machines; the task page shows each
  place with its known path on every machine (not one "this machine" selector).
- **Place picker**: an open list with a search box on top (not a collapsed dropdown).
- **Revert a document to an earlier revision** from the web: creates a new revision whose
  body is that revision's (`author: owner`, message `revert to r<n>`); history is never
  rewritten; plan items and open comments are handled as for any new revision; the same
  `base_revision` conflict check. Agents do the same with `doc_get(revision)` + `doc_write`.
- No owner journal entries in the web (not needed now).

## Owner feedback on the mockup (v0.5)

- **No task merging.** Removed: `closed_reason: merged`, `merged_into`, `task_create(from_tasks)`.
  Reshaping tasks is done by an agent editing them on request. Kept: a task created from
  todos (`task_create(from_todos)`, todos → `in_task`) — in MCP and in the web.
- **Plan items ticked by the owner.** The owner ticks/unticks and adds notes in the web; an
  agent may tick on the owner's behalf. Each tick and note records `by: owner | agent`
  (`plan_item_set(..., by?)`, default `agent`); the UI shows who did it.
- **Todos are created in the web** too (text, note, places, tags).
- **Place picker with search** wherever a place is chosen or filtered (users have 20+ places).

## Waiting, machines, retention (v0.4)

- `review_wait(task?, doc?, timeout_s?)` — blocks until a round for that document (or any
  document of the task, or anywhere when both omitted) is submitted, then returns it like
  `review_pending`; on timeout returns `{waiting: true}` so the agent can call again or do other
  work. `timeout_s` default 300, max 900 (server config). The owner can always interrupt the
  agent from the chat.
- Machine name: clients send `machine` from `AGENT_TASKS_MACHINE` (or the client config's
  `machine` key) as is, else the short host name (`laptop`, not `laptop.local`).
  Containers set it explicitly (their host names are random). Same rule for the hook and skills.
- Retention of archived tasks: config, default 90 days.
- `workspace_root` stays a client/server config value; the default `~/agent-work` is only a
  default — users point it wherever they keep work folders.

## Clarifications (v0.2)

- `quote` is a fragment of the markdown source of the revision, not of the rendered text.
- Anchor offsets count Unicode code points. The quote decides; the server computes
  `prefix`/`suffix` itself when a client sends only the selection.
- Comment and round ids are integers, global across tasks.
- `doc_write` with a body identical to the latest revision creates nothing and returns
  `unchanged: true`.
- A new plan item takes its initial `done` from the body (`[x]`); a matched item keeps its
  stored state.
- Only `open` comments can become `outdated`.
- One draft round per document. On submit, its comments are re-anchored to the latest
  revision.
- Responses may carry extra fields (`workspace`, `diff_url`, `line`, `current`); clients ignore
  unknown fields.
- Search also indexes round summaries.
- The SPA is built with `base: '/'`. The JSON API used by the UI is listed in
  `docs/backend-design.md` ("JSON API"); it is internal to this repo, not part of the contract.

## Changelog

- v0 (2026-11-15): first version.
- v0.1 (2026-11-15): session hook endpoint and the plugin's SessionStart hook.
- v0.2 (2026-11-15): clarifications from the backend implementation.
- v0.3 (2026-11-15): places instead of repo tags, machine-learned locations, todos, merging.
- v0.3.1 (2026-11-15): todo links and place details from the backend implementation.
- v0.4 (2026-11-15): review_wait, machine name rule, retention default 90 days.
- v0.5 (2026-11-15): no merging; owner ticks with `by`; todos from the web; searchable place
  picker. Earlier sections mentioning merging are superseded by this one.
- v0.6 (2026-11-15): search deep links, bulk status, owner revisions and renames, all
  machines' paths, open place list.
- v0.6.1 (2026-11-15): "owner" defined; tags are free strings; session hook without a remote,
  with `machine`, todos and proxy headers; `quote` is markdown source.
- v0.7 (2026-11-15): the owner's approval of a revision (web, MCP, review_wait, session hook);
  live updates of the web UI.
