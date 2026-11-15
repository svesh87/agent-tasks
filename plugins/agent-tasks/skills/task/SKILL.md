---
name: task
description: Start, resume, park or close work on a task kept on the agent-tasks server; list the tasks and todos of the current repository; take a task (T-12) or a todo (D-3); form a new task from todos. Use at the start of work in a repository, when the owner (the person you work for) names a task or todo, asks what is open here ("что тут есть", "what's open"), or says to pause, park or close something.
---

# Tasks on the agent-tasks server

A task outlives a session and a repository. It holds documents (plan, research, notes), the
owner's reviews, plan progress, a journal, and the places (repositories, folders) it works
in. The owner is the person you work for; they review in the server's web UI. Scratch files
live in the task's workspace folder on disk.

## Where am I

- Machine name: `$AGENT_TASKS_MACHINE` if set, else the short host name (`hostname -s`).
  The session hook uses the same rule, so locations do not split between names. Containers
  have random host names and must set `AGENT_TASKS_MACHINE`.
- Place: `cwd` is the repository root (`git rev-parse --show-toplevel`), or the current
  directory outside git; `remote` is `git remote get-url origin` if there is one.
- `place_here(cwd, remote, machine)` returns the place, its tasks and its open todos, and
  tells the server where the place lives on this machine.
- If the session started with lines from the agent-tasks hook ("agent-tasks: place …"), you
  already have that list; still call `place_here` before creating or linking anything. The
  hook is optional: everything here works through MCP alone.

## List what is open here

Show tasks compactly (id, status and what it waits on, title, pending reviews), then the open
todos (`D-n text`). For older or archived work: `task_search(query, place, status:
["archived"])`.

## Take a task (T-n)

1. `task_get(id, machine)`: documents (with `approved` when the owner approved the latest
   revision, which is their go on it), plan progress, last journal entries, pending rounds,
   places with their paths on this machine, `workspace`.
2. Read the latest `context` entries (`journal_get(task, kind: "context", limit: 3)`) and
   resume from them. Check them against the real state (git status, files, the live system)
   before acting.
3. If submitted review rounds are waiting, handle them first with the `review` skill.
4. If the work continues in a repository that is not among the task's places, add it:
   `task_update(places_add: [{place, role: "touched", note}])` (`primary` if it becomes the
   main one).
5. Workspace: create the folder if missing (`mkdir -p`, expanding `~`). Scratch files,
   scripts and dumps go there, not into the repository.
6. If the next steps are non-trivial, use the `plan` skill.

## Take a todo (D-n)

Find it (`todo_list(place)` or by id). If it is small and clear, do it, then
`todo_update(id, state: "done")`. If it needs planning, `task_create(title, from_todos:
["D-n"], places)`, then the `plan` skill.

## New task

Search first (`task_search(query, place)`) to avoid duplicates. Then `task_create(title,
summary, places: [{place: "<host/path>", role: "primary"}], tags)`. Write the title in the
owner's language. Use tags only for topics and areas (`topic:…`, `area:work|personal`), never
for repositories. Give the owner the returned `url`.

## Form a task from several todos

Confirm the selection with the owner, then `task_create(…, from_todos: [open todo ids])`.
The todos move to `in_task` and their places join the task. Tasks are never merged; if the
owner wants tasks reshaped, edit them as asked (titles, documents, places, status).

## Plan items

Tick steps with `plan_item_set(task, slug, item, done, note)`. The owner may also tick or
note items in the web UI; `task_get` and `doc_get` show who did it (`done_by`,
`notes[].by`). Do not untick what the owner ticked without asking.

## Change status

- `active`: being worked on.
- `waiting`: always with `waiting_on`, saying who or what it waits for ("owner review",
  "vendor support").
- `later`: parked. First write a `context` entry: why it is parked and what the next step
  would be.
- `archived`: only with the owner's agreement. Write a final `context` entry, then
  `task_update(status: "archived", closed_reason: "done" | "cancelled")`.

## Before the session ends or the context is compacted

`journal_append(task, kind: "context", body, source)`: where the work stands, what is next,
what is risky or unverified, which files in the workspace matter. Write it for a reader who
has never seen this chat.
