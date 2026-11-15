---
name: review
description: Collect and address the review rounds the owner (the person you work for) submitted on agent-tasks documents. Answer every comment, write the revised document, resolve what is handled, mark the round addressed and send the links back. Use when the owner says a review is submitted or done ("посмотрел", "ревью готово"), when task_get or the session hook shows pending rounds, and before continuing work on a reviewed document.
---

# Address a review round

The owner reviews documents in the web UI: comments anchored to quoted text, general
comments, and an optional round summary. Submitting the round means it is ready for you.
Drafts are invisible to you.

## Steps

1. `review_pending(task)` (or without `task` for all tasks) returns the rounds with their
   comments, anchors (`quote`), the reviewed revision and the summary.
2. Read the document: `doc_get(task, slug)` for the latest revision, and the reviewed
   revision if it is older, so you read each quote in its context.
3. Decide on every comment, including the round summary:
   - if you agree, change the document (or the work);
   - if you disagree, explain why, briefly, with the evidence;
   - if it is a question, answer it; if the answer is a real fork, ask the owner (question
     tool) instead of deciding alone.
   Check claims in comments against the code, data or live state before accepting or
   rejecting them.
4. Write **one** new revision with all changes: `doc_write(task, slug, body, message:
   "addresses review round N")`. Keep the text of plan items stable where the step itself
   did not change, so their ticks and notes carry over.
5. Reply to each comment with `comment_reply(comment, body, resolve)`: what changed (quote
   the new wording or name the step) or why nothing did. Set `resolve: true` only when the
   comment is fully handled; leave it open when it waits for the owner or for later work,
   and say so.
6. `round_addressed(round, message)` with one line for the owner.
7. Tell the owner the document `url`, the `diff_url` from `doc_write`, what changed and what
   is still open. Record the owner's choices as `decision` journal entries.

## Waiting for the next round

After sending the links, if there is nothing else to do, wait with `review_wait(task, doc)`
in a loop. It blocks until the owner submits a round and returns it like `review_pending`.
When its timeout passes it returns `{waiting: true}`; call it again, as many times as
needed. Tell the owner in chat that you are waiting; they can interrupt you there.

`review_wait` with `doc` also returns when the owner approves the latest revision: the result
carries `approved` (`{revision, at}`), possibly together with rounds. Handle the rounds as
above, then treat the approval as the owner's go (see the `plan` skill). The approval stays
until a new revision is written, so do not call `review_wait` again just to see it.

`timeout_s` defaults to 300 s, but the server may cap it lower (an auth proxy in front of
the server often closes requests after about 90 s), and the MCP client has its own
tool-call timeout. A short wait is fine because the loop makes up for it. If calls are cut
off, pass a smaller `timeout_s`.

## Rules

- Never skip a comment silently, and never resolve one you did not act on.
- Comments that went `outdated` (their text is gone) still get a reply.
- A review is feedback on the document. It does not by itself approve execution: wait for an
  explicit "go" — a "yes" in the chat or the owner's approval of the latest revision (see the
  `plan` skill).
