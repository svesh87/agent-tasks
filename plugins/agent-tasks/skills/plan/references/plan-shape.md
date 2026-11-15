# What a plan may contain

A menu, not a form. Take what the work needs; a three-step change needs three lines.

- **Goal**: one or two sentences on what will be true when this is done.
- **Decisions**: the owner's answers to the forks, stated as settled facts ("Backups go to
  NFS, not SMB").
- **Steps**: `- [ ]` lines, in order. For code: files and what changes in them. For
  configuration or infrastructure: what is changed where, and how it is applied.
- **Stale text**: comments and documentation this change makes wrong, with file and place.
  They go stale silently; tests do not catch them.
- **Checks**: the repository's actual commands (tests, linters, dry runs) and what output
  counts as success. If the repository has none, what is checked instead. Do not introduce
  new tooling on your own initiative.
- **Owner-only checks**: anything only the owner can do or see (live hardware, production,
  accounts). Give them a section of their own so they are not lost.
- **Defaults**: small choices you made, each marked as changeable at review.
- **Risks and rollback**: what can break, how you would notice, and how to undo it.

Not in a plan: an open-questions section (ask instead), filler ("improve robustness"),
future-proofing nobody asked for.

## Asking well

- `AskUserQuestion` (Claude Code) takes a few questions per call, each with a short header
  and 2–4 options; the owner can always answer in free text. Batch related questions; put the
  recommended option first and say why in its description.
- An agent runner may offer its own question tool (an MCP tool that shows the owner 1–4
  questions with options and waits for the answer); use it the same way.
- In plain chat: number the questions, give lettered options, and say which you recommend.
- Group questions by topic so the owner can answer one batch while you prepare the next.

## A small example

```markdown
# Plan: rotate the DNS provider token used by Terraform

Goal: Terraform runs with a token scoped to DNS only; the old one is revoked.

Decisions: scope DNS:Edit (owner, round 1); expiry one year.

- [ ] Create the token in the provider's dashboard (owner) and put it into secrets
- [ ] `terraform plan` shows no changes with the new token
- [ ] Revoke the old token (owner), `terraform plan` still clean

Owner-only: creating and revoking tokens in the provider's dashboard.

Defaults: token name `terraform-dns-2026` (changeable).
```
