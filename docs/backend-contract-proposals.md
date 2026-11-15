# Contract proposals from the backend

Points the implementation had to decide that CONTRACT.md v0.1 does not fix. Each is implemented
as described; adopt into the contract or ask for a change.

1. **Anchor offsets are Unicode code points** of the markdown source (not bytes, not UTF-16).
   The quote is authoritative: the server takes `[start, end)` if it holds the quote, else the
   occurrence nearest to `start`, and computes `prefix`/`suffix` itself. The UI may send only
   `{quote, start}`.
2. **Comment and round ids are integers**, global across tasks (`comment_reply(comment: 17)`,
   `round_addressed(round: 4)`). Plan item ids are per document (`i3`).
3. **Identical body → no revision.** `doc_write` with the same body as the latest revision
   returns `unchanged: true` and the current revision n.
4. **Box state of new items.** A new plan item starts with the box written in the body
   (`- [x]` = done); matched items keep their stored state whatever the body says.
5. **Outdated applies to open comments only.** A resolved comment whose text disappears stays
   `resolved` (its `outdated_in` is still recorded).
6. **One draft round per document.** Opening a draft returns the existing one. A draft made on an
   older revision is re-anchored to the latest revision on submit. A round needs at least one
   comment or a summary to be submitted. Round numbers count drafts that were later discarded
   only if they were the last one.
7. **Owner actions on comments** (REST only): resolve, reopen (also from `outdated`), reply.
   Draft comments can be edited and deleted; submitted ones cannot.
8. **Extra result fields:** `task_*` results carry `workspace` (`<workspace_root>/<id>`, not
   expanded); `doc_write` carries `diff_url` (previous → new revision), `reanchored` (count) and
   `created`; `doc_get` carries `latest_revision` and the revision list; comments carry `line`
   and `current` (position in the newest revision) for the agent's convenience.
9. **Search also indexes round summaries** (`round:<id>`). MCP snippets are plain text with
   matches in `**bold**`; REST snippets add `highlights` as rune ranges.
10. **Session hook:** besides `repo`, a `cwd` with a `T-<n>` path component (a workspace folder)
    adds that task, even if archived. At most 20 tasks, active → waiting → later. On any server
    error the body is empty, as the hook expects.
11. **Token config:** `AGENT_TASKS_TOKENS="name=secret,name2=secret2"` or a tokens file; names
    appear in logs, secrets never. No tokens → `/mcp` and `/hooks` answer 503.
12. **Web build:** the Dockerfile builds `web/` with `npm ci && npm run build` and embeds
    `web/dist`. The current Vite config uses `base: './'`; the server copes (deep links asking
    for `…/assets/x.js` get `/assets/x.js`), but `base: '/'` for the production build is
    cleaner. UI routes from the contract (`/t/<id>/d/<slug>/diff/<a>..<b>` etc.) must be
    client-side routes; the server returns `index.html` for any unknown path.
13. **REST API** for the UI is listed in `docs/backend-design.md` (section "JSON API").

## v0.3 (places and todos)

14. **Todo web links:** `/todos/<id>` for one todo and `/todos?place=<key>` for the list (MCP
    results carry them as `url` / `todos_url`). The UI needs these client routes.
15. **Place keys in inputs** accept any remote form (`git@host:a/b.git`, `https://…`) and are
    normalised; `<machine>:/abs/path` (no `@` before the colon) is a path key. Unknown places
    named in `places`/`places_add`/todo `places` are created (without locations).
16. **place_here location rule:** a `cwd` inside an already known location of the same place
    on the same machine refreshes that location instead of adding a subfolder; without a remote,
    the longest known containing location wins. Agents should pass the repository top level as
    `cwd` when they can.
17. **task_get takes optional `machine`**: places then carry only that machine's locations
    (empty = not present there). Without it, all machines' locations are returned.
18. **Task from todos:** `from_todos` accepts only open todos; a todo cannot be set to
    `in_task` through `todo_update`; reopening an `in_task` todo clears its task; todos' places
    join the new task as `touched`. `task_create` takes an optional `source` for the journal
    entry. (Merging was removed in v0.5.)
19. **Session hook output** lists, per task, its other places with the local path or "not on
    this machine", then up to 20 open todos of the place. `machine` missing → a git place is
    still found by `repo` but no location is recorded.
20. **The `repo:` tag namespace has no special meaning any more** (free tag); no data
    migration. `0001_init.sql` was edited in place (no deployed DB yet).

## v0.4 (review_wait)

21. **review_wait returns at once when matching rounds are already pending**, not only on a
    new submit. Otherwise an agent that missed a submit would wait for nothing. `doc` requires
    `task`. On timeout the result is `{waiting: true, waited_s, hint}`.
22. **Client timeout settings belong in the setup instructions.** Codex's default
    `tool_timeout_sec` is 60 s, so the plugin and skills docs should tell users to set
    `tool_timeout_sec = 960`. For Claude Code, set `"timeout": 960000` on the server entry (the
    idle limit for HTTP servers is 5 min). Skills should pass a `timeout_s` below the client
    limit: 300 with the settings above, 50 when the limit is unknown.
23. **Behind Cloudflare** or a similar auth proxy, a response that sends nothing for 100 s
    gets a 524. The server sends progress notifications every 20 s only if the client passed a
    progress token, and we have not checked whether Claude Code or Codex do. Until this is
    tested on a live tunnel, run the server with `-review-wait-max 90` there. The alternative is
    an always-on SSE keep-alive, which needs a go-sdk change or a custom transport.

## v0.5 (owner feedback): answers to the web's `docs/contract-proposals.md`

24. **Plan item attribution in JSON:** items have `done_by` (`owner|agent`, who ticked or
    unticked last) and `notes[].by`. Before v0.5 the note field was `author`; it was renamed to
    match the contract's `by`. A new `- [x]` line takes the revision's author (`import` counts as
    owner).
25. Implemented from the web proposals:
    1. facets: `status_counts`/`tag_counts` on tasks, `state_counts`/`tag_counts` on todos;
    2. list rows: `plan_progress` of the main plan document, `doc_count` (not `documents`, which
       on the task page is the list of documents), `submitted_rounds`, `draft_rounds`;
    3. `machines` in `/api/config`;
    4. MCP comment `url` with `?c=<id>`;
    7. `addressed_revision` on rounds.

    Not done: 5 (anchor for the requested revision) and 6 (wording about `quote`; the server
    already treats `quote` as source text).

