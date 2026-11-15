---
name: plan
description: Plan non-trivial work together with the owner (the person you work for) before doing it. Understand the task, ask the owner many questions in batches, publish the plan as a document on the agent-tasks server, go through the owner's review rounds, start only on an explicit go, then tick plan items while working. Use for anything larger than a couple of edits, when the owner asks for a plan ("plan this", "давай план", "составь план"), and when an unforeseen fork appears in the middle of work.
---

# A plan, before the work

Non-trivial work starts with understanding, questions and a plan the owner has reviewed. The
owner is the person you work for; they review your documents in the server's web UI.
Execution begins on the owner's explicit "go", not on silence and not on a question.

The process is deliberately light: no required template, size or stages. Shape the plan to
the work. What matters is that the owner sees clearly what will happen and why, and has
settled every choice that matters before you start.

Everything lives on the agent-tasks server (MCP tools below). If the server is not
connected, say so and ask the owner how to proceed instead of improvising storage.

## 1. Understand

- Find out yourself what code, data, docs and the live state can tell you. Ask the owner only
  what only the owner knows.
- Find or create the task (see the `task` skill): `place_here` for where you are,
  `task_search` for related work, `task_get` for an existing task, `task_create` with
  `places` for a new one. Read the latest `context` journal entries of an existing task.

## 2. Ask a lot, in batches

- Use the structured question tool when there is one: `AskUserQuestion` in Claude Code, or
  the question tool an agent runner provides. Otherwise ask in chat as a numbered list with
  concrete options.
- Ask in batches as groups of questions become ready. Do not wait until you have all of
  them, and do not drip them one at a time. Several rounds are normal.
- Ask everything that affects the result, not only the big things. Tiring the owner during
  preparation is cheap; interrupting them mid-implementation is not.
- Offer concrete options with their consequences, the one you recommend first. Do not ask
  what you could find out by reading.
- Ask about **forks** **before** writing the plan. A fork is architecture, scope, external
  configuration, or anything where two readings lead to materially different work. The
  answers go into the plan as decisions and into the journal as `decision` entries.
- Minor choices you make yourself go into a **defaults** section of the plan, each one
  changeable at review.
- The plan has **no open-questions section**. If a question is still open, the plan is not
  ready: ask it.

## 3. Write the plan

- `doc_write(task, slug: "plan", body, message)`: markdown, in the owner's chat language.
- Steps are task-list lines `- [ ] …`; the server turns them into plan items with stable
  ids. Make each step checkable: what changes where, and how it will be verified (the
  repository's real commands; if there are none, say what is checked instead).
- Usually worth having: the goal, the decisions taken, the steps, the defaults, and the
  checks only the owner can do (live systems, hardware). See
  [references/plan-shape.md](references/plan-shape.md) for a fuller checklist. Treat it as a
  menu, not a form.
- Send the owner the `url` from the result, as is, with two or three lines on what to look at.
- `task_update(status: waiting, waiting_on: "owner review")` and a `context` journal entry.
- Then wait for the review. If you have nothing else to do, call `review_wait(task, doc:
  "plan")` in a loop: it returns the submitted round, or `{waiting: true}` when its timeout
  passes, and then you call it again; it also returns when the owner approves the latest
  revision (`approved`). Say in chat that you are waiting; the owner can stop you
  there at any time. If you have other useful work, do it and check `review_pending` later
  instead. See the `review` skill for timeouts.

## 4. Review rounds until "go"

- The owner reviews in the web UI and submits a round. When they say so (or a new session
  shows pending rounds), handle it with the `review` skill: answer every comment, write the
  next revision, mark the round addressed, send the link and the diff link again.
- Repeat until the owner explicitly says to go. A question is not approval. The owner's "no"
  holds until they come back to it themselves.
- "Go" is either an explicit "yes" in the chat or the owner's **approval** of the latest
  revision in the web UI: `review_wait(task, doc)` returns `approved`, and `doc_get` /
  `task_get` show `approved` on the document (the session hook shows `plan r<n> approved`).
  An approval counts only for the revision it names; writing a new revision removes it.
- An approval may arrive together with a round ("approve with comments"). Answer the comments
  first. If they only refine details, start the work without a new revision. If they change
  the plan, write the new revision, tell the owner, and wait for a new go.

## 5. Execute

- `task_update(status: active)`. Work through the plan.
- Tick items with `plan_item_set(task, slug, item, done: true, note: "what was done, how it
  was checked")`. Never rewrite the document just to tick boxes. Use notes for partial
  progress and blockers, too. When the owner tells you in chat that a step is done, tick it
  with `by: "owner"`. The owner may also tick or note items in the web UI, so check the
  current state (`doc_get`) before working on a step.
- A material change of plan means a new revision and a word to the owner. An unforeseen fork
  means stop and ask, unless the code, data or live state answers it; then look first.
- `journal_append`: `log` for significant actions, `decision` for the owner's choices and
  why, `context` at milestones and before the session ends or the context gets long. Write
  context entries so that a fresh session without this chat can resume.
- Scratch files, scripts and dumps go to the task's workspace folder (`workspace` in
  `task_get`), never into the repository and never onto the server.

## Work that touches another repository

- Add it to the task: `task_update(places_add: [{place: "<host/path>", role: "touched",
  note}])`.
- Find its path on this machine (`task_get(id, machine)` or `place_get(key)`). If it is not
  present here, ask the owner where it is or whether to clone it.
- Delegate the change to a subagent. Give it the repository path, the goal, the plan items it
  covers, and the instruction to read that repository's `AGENTS.md` / `CLAUDE.md` first and
  follow its rules. Review what comes back before ticking items.

## Done

When all items are done or consciously dropped (with a note why), write a final `context`
entry and give the owner a short report: what was verified and what was not. Archiving is the
owner's call (`task` skill).
