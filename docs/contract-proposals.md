# Contract proposals from the web UI and the plugin

Found while building the web UI (`web/`) and the plugin (`plugins/agent-tasks/`). Status as
of CONTRACT.md v0.6 and the JSON API in `docs/backend-design.md`.

## Done

- List facets (`status_counts`, `tag_counts`, `state_counts`) and per-row facts
  (`plan_progress`, `doc_count`, `submitted_rounds`, `draft_rounds`) in `GET /api/tasks` and
  `GET /api/todos`.
- Places show their paths on every machine; no "this machine" selector in the web.
- Deep links: `…/r/<n>?c=<comment>`, `?at=<start>-<end>` (code points), `?round=<id>`,
  `?j=<journal entry>`; search hits carry `kind` and `url`; todos matching a search come with
  the task results.
- `addressed_revision` on rounds ("answered in rN" + diff link).
- Owner actions in the web: ticks and notes (`done_by`, `notes[].by`), new todos, bulk status
  changes for tasks and todos, renaming a task and editing its summary, editing a document
  (`PUT` with `base_revision`, 409 on a newer revision), reverting to an earlier revision.
- Tags with counts across tasks and todos (`GET /api/tags`) for the tag autocomplete.
- No task merging (v0.5).
- Machine name rule: `AGENT_TASKS_MACHINE` as is, else the short host name (v0.4).

## Open

1. **Where an anchor is shown.** A comment is shown on its reviewed revision and on
   `current.revision` only, so a comment on r1 that still matches in r3 is not highlighted when
   the owner opens r2. Acceptable for now; if it matters, `GET …/docs/{slug}?rev=n` could
   return the anchor for the requested revision.
2. **What `quote` holds.** The UI maps a selection in the rendered document to markdown source
   offsets and sends the source slice as `quote` (a selection across `**bold**` includes the
   asterisks). Proposal: say in the contract that `quote` is the source text between `start`
   and `end`. The UI converts UTF-16 indices to code points before sending
   (`web/src/lib/markdown.ts`).
3. **Hook settings.** `~/.config/agent-tasks/config` (or `$AGENT_TASKS_CONFIG`, respecting
   `XDG_CONFIG_HOME`) with `url`, `token`, `machine` and repeated `header = Name: value`
   lines; environment variables `AGENT_TASKS_URL`, `AGENT_TASKS_TOKEN`, `AGENT_TASKS_MACHINE`,
   `AGENT_TASKS_HEADERS`, `AGENT_TASKS_HEADERS_FILE`. Proposal: name them in the contract so
   other clients share them.
4. **Hook without a git remote.** The endpoint resolves path places and `T-<n>` workspace
   folders from `cwd`, so the hook calls it with an empty `repo` too. Proposal: update the v0.1
   sentence ("no git, no remote … prints nothing").
5. **Output size.** Claude Code caps a hook's stdout at 10,000 characters; the hook cuts at
   9,000, the server lists at most 20 tasks and 20 todos.
6. **Not in the web yet**: journal entries by the owner (not needed for now), creating a new
   document from the web.
