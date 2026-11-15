---
name: todo
description: Record, list and close todos on the agent-tasks server. A todo is a short standalone note for later that may grow into a task or be dropped. Use when the owner (the person you work for) says "add a todo", "note this for later", "запиши в дела", "что у нас в делах", or asks to mark a todo done or drop it.
---

# Todos

A todo (`D-n`) is one line, optionally with a note, kept for later. Todos stand alone and as
a rule belong to no task. They are not plan items: plan items are steps inside a task's
document.

## Record

Record a todo only when the owner asks; otherwise suggest it.

`todo_add(text, note, places, origin_task, tags, source)`:
- `text`: one line in the owner's words and language; details go to `note`.
- `places`: the place key if the todo concerns a repository or folder (the current one from
  `place_here`, or another `host/path`); empty when it is not tied to a place.
- `origin_task`: only if the idea really came out of that task's work. It records where the
  idea came from and does not make the todo part of the task; otherwise leave it empty.
- `tags`: topics or areas, if useful (`topic:backup`, `area:work`).

Reply with the id and the text as recorded.

## List

`todo_list(place, state, query, tags)` returns open todos by default. `place: "none"` lists
todos without a place. Show them as `D-n text` lines with their places.

## Close or reopen

`todo_update(id, state: "done" | "dropped" | "open")`. To edit, pass `text`, `note`,
`places_add/remove`, `tags_add/remove`.

## Grow into a task

`task_create(title, from_todos: ["D-n", …], places)`: the todos move to `in_task` and their
places join the task. See the `task` skill.
