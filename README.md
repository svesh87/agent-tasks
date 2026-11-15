# agent-tasks

[Русская версия](README.ru.md)

Coding agents such as Claude Code and Codex forget everything between sessions: the plan you
agreed on, what is already done, why a choice was made. Real work also spans several
repositories and machines. And reviewing an agent's plan in a chat is awkward: you cannot point
at a line, and each new version buries the last one.

agent-tasks is a small self-hosted server that keeps this work outside any one repository.
Agents connect over MCP. They write plans and notes as documents, tick off plan steps, and keep
a journal to resume from. You, the person the agent works for, read these documents in a web
UI and comment on selected text, much like reviewing a pull request. The agent then picks up
your review and answers each comment.

The server is one Go binary with an SQLite file and a built-in web UI. A plugin compatible
with Claude Code and Codex provides the skills and session hook that teach agents how to use
it.

## Concepts

- **Task**: a piece of work with a title, a status (`active`, `waiting`, `later`, `archived`),
  free-form tags (`topic:…`, `area:…`) and the places it touches. Ids look like `T-12`.
- **Document and revision**: markdown documents in a task (plan, research, notes). Each save
  creates a new revision; old ones never change. You can compare any two and go back to an
  older one.
- **Review round and comment**: your comments on one revision, either on selected text or on
  the whole document. You collect them in a draft and submit them together. The agent replies,
  resolves them and writes the next revision. If the commented text is gone, the comment is
  kept and marked outdated.
- **Plan item**: each `- [ ]` line in a document. An item keeps its id across revisions. The
  agent or you can tick it and add notes without editing the document.
- **Journal**: short entries the agent writes. *Context* says where the work stands, so a new
  session can pick it up. *Log* records what was done. *Decision* records what was decided and
  why.
- **Place**: where work happens. Either a git repository, known by its `origin` remote (the
  same on every machine), or a local folder. The server remembers where each place lives on
  each of your machines.
- **Todo**: a one-line note for later (`D-7`), not tied to a task. You can turn several todos
  into a task.
- **Workspace folder**: `<workspace_root>/<task id>` on disk, for the agent's scratch files,
  scripts and data dumps. It keeps them out of your repositories. To use it on several
  machines, sync it with any file sync tool.

## How a task goes

1. You describe the work. The agent, using the `plan` skill, reads the code and asks you
   questions in batches until every choice is settled.
2. It creates a task, writes the plan as a document and sends you a link.
3. You open the link, comment on selected text, add general remarks and submit the review.
4. The agent answers each comment, writes the next revision and marks the round as handled.
   This repeats until you say "go".
5. The agent works through the plan. It ticks items, adds notes and keeps the journal, so a
   later session, even on another machine, continues where the last one stopped.
6. When the work is done, the task is archived. Archived tasks are deleted after a retention
   period.

You can start in two ways. **From a repository**: when a session opens, the session hook
lists the tasks and todos of that repository. **From a task folder**: open a task's workspace
folder, and the hook shows that task with the paths to its repositories on this machine.

Ideas that are not ready yet go into todos ("add a todo: …"). Later you select a few in the
web UI and turn them into a task.

## Quick start

### 1. Run the server

Create a token file with one `name=secret` per line. Names appear in logs; secrets never do.

```sh
mkdir -p ~/.config/agent-tasks
printf 'laptop=%s\n' "$(head -c 24 /dev/urandom | base64 | tr -d '/+=')" > ~/.config/agent-tasks/tokens
chmod 644 ~/.config/agent-tasks/tokens     # the container runs as uid 10001
```

Run the image (or build your own with `docker build -t agent-tasks .`):

```sh
docker run -d --name agent-tasks -p 127.0.0.1:8080:8080 \
  -v agent-tasks-data:/data \
  -v ~/.config/agent-tasks/tokens:/run/tokens:ro -e AGENT_TASKS_TOKENS_FILE=/run/tokens \
  ghcr.io/svesh87/agent-tasks:<tag>
```

Or download a release binary and run
`AGENT_TASKS_TOKENS_FILE=~/.config/agent-tasks/tokens ./agent-tasks serve`.

Open <http://localhost:8080>. The web UI has no login of its own, so keep it on localhost or
put it behind an auth proxy (see below).

### 2. Connect Claude Code

```sh
claude plugin marketplace add https://github.com/svesh87/agent-tasks
claude plugin install agent-tasks@agent-tasks
claude mcp add --transport http --scope user agent-tasks http://localhost:8080/mcp \
  --header "Authorization: Bearer <secret>"
```

For the session hook, create `~/.config/agent-tasks/config`:

```ini
url = http://localhost:8080
token = <secret>
```

### 3. Connect Codex

Install the plugin from this repository's marketplace:

```sh
codex plugin marketplace add https://github.com/svesh87/agent-tasks
codex plugin add agent-tasks@agent-tasks
```

Connect the MCP server:

```toml
# ~/.codex/config.toml
[mcp_servers.agent-tasks]
url = "http://localhost:8080/mcp"
bearer_token_env_var = "AGENT_TASKS_TOKEN"
tool_timeout_sec = 330
```

The `SessionStart` hook uses the same config as Claude Code:

```ini
# ~/.config/agent-tasks/config
url = http://localhost:8080
token = <secret>
```

Update the plugin and verify the installation with:

```sh
codex plugin marketplace upgrade agent-tasks
codex plugin list
```

Start a new Codex session after installing or updating. Run `/hooks` to review and trust the
plugin hook when Codex asks; its trust is tied to the current hook hash.

### Behind an auth proxy

To reach the server from several machines, put it behind a proxy that handles login, for
example Cloudflare Access:

- One application for the web UI, where people log in. Start the server with
  `-web-auth-header Cf-Access-Authenticated-User-Email` so it refuses requests that bypass the
  proxy, and with `-base-url https://tasks.example.com` so links use the public name.
- One application for `/mcp` and `/hooks/*` that accepts service tokens. Agents send the
  token's headers along with the server's bearer token.
- Proxies drop requests that stay silent for about 100 seconds, so cap long waits with
  `-review-wait-max 90`.

Claude Code (`claude mcp add-json agent-tasks '<json>'` or `.mcp.json`; `${VAR}` is read from
the environment; `timeout` is in milliseconds):

```json
{
  "type": "http",
  "url": "https://tasks.example.com/mcp",
  "headers": {
    "Authorization": "Bearer ${AGENT_TASKS_TOKEN}",
    "CF-Access-Client-Id": "${CF_ACCESS_CLIENT_ID}",
    "CF-Access-Client-Secret": "${CF_ACCESS_CLIENT_SECRET}"
  },
  "timeout": 120000
}
```

Codex:

```toml
[mcp_servers.agent-tasks]
url = "https://tasks.example.com/mcp"
bearer_token_env_var = "AGENT_TASKS_TOKEN"
env_http_headers = { "CF-Access-Client-Id" = "CF_ACCESS_CLIENT_ID", "CF-Access-Client-Secret" = "CF_ACCESS_CLIENT_SECRET" }
tool_timeout_sec = 120
```

Session hook: add the same headers to `~/.config/agent-tasks/config`:

```ini
header = CF-Access-Client-Id: <id>
header = CF-Access-Client-Secret: <secret>
```

## The web UI

- **Search** covers tasks, documents, comments, review summaries, the journal and todos, and
  matches Russian and English word forms. Each result shows what matched and opens it
  directly: the document at the right revision with the match highlighted, the comment, the
  journal entry.
- **Tasks**: filter by status, place and tags; change the status of several tasks at once;
  rename a task and edit its summary in place; see each place's path on every machine.
- **Documents**: comment on selected text, submit a review round, reply. Tick plan items and
  add notes; the UI shows whether you or the agent did it. Edit the latest revision as
  markdown with a preview; if a newer revision appears meanwhile, you are told and nothing is
  overwritten. Browse the history, compare revisions, go back to an older one.
- **Todos**: quick add, places and tags with autocomplete, mark several as done, dropped or
  open again, turn several into a task.
- English and Russian, light and dark theme, works on a phone.

## Configuration

| Flag | Environment | Default |
|---|---|---|
| `-listen` | `AGENT_TASKS_LISTEN` | `:8080` |
| `-db` | `AGENT_TASKS_DB` | `agent-tasks.db` (image: `/data/agent-tasks.db`) |
| `-base-url` | `AGENT_TASKS_BASE_URL` | `http://localhost:8080` |
| `-tokens-file` | `AGENT_TASKS_TOKENS_FILE` | none (`name=secret` per line) |
| — | `AGENT_TASKS_TOKENS` | none (`name=secret,name2=secret2`) |
| `-retention-days` | `AGENT_TASKS_RETENTION_DAYS` | `90` (`0` keeps archived tasks) |
| `-review-wait-max` | `AGENT_TASKS_REVIEW_WAIT_MAX` | `900` seconds |
| `-workspace-root` | `AGENT_TASKS_WORKSPACE_ROOT` | `~/agent-work` |
| `-web-auth-header` | `AGENT_TASKS_WEB_AUTH_HEADER` | none (no check) |

Tokens are never accepted as flags. Backups, markdown export and the full design are in
[docs/backend-design.md](docs/backend-design.md). The rules the server and the skills share are
in [CONTRACT.md](CONTRACT.md).

## Skills and the session hook

This repository is a plugin marketplace compatible with Claude Code and Codex. It contains
one plugin, `agent-tasks` (`plugins/agent-tasks/`). The skills use only this server's MCP
tools:

- `plan`: understand the task, ask, write the plan, revise it after your review, start work
  when you say go;
- `task`: find, resume, park or close tasks; list the tasks and todos of the current
  repository;
- `review`: collect your submitted round and answer every comment;
- `todo`: record, list and close todos.

The `SessionStart` hook runs in Claude Code and Codex when a session starts, resumes or is
compacted, and prints the tasks and open todos of the current place. It reads
`~/.config/agent-tasks/config` (`url`,
`token`, `machine`, and any number of `header = Name: value` lines) or the environment
(`AGENT_TASKS_URL`, `AGENT_TASKS_TOKEN`, `AGENT_TASKS_MACHINE`, `AGENT_TASKS_HEADERS`,
`AGENT_TASKS_HEADERS_FILE`). The machine name defaults to the short host name; set it in
containers, where host names are random. The hook passes secrets to `curl` on stdin, not on
the command line. Without settings, or when the server is down, it prints nothing.

## Development

```sh
cd web && npm ci
npm run dev                                          # UI on sample data, http://localhost:5173
AGENT_TASKS_API=http://127.0.0.1:8080 npm run dev    # UI against a running server
npm run build                                        # web/dist, embedded by the Docker build
npm run build:single                                 # a one-file demo on sample data

# Go checks without a local Go toolchain (from the repository root)
docker run --rm -v "$PWD":/src -w /src -e CGO_ENABLED=0 golang:1.27.2-alpine3.24 \
  sh -c 'go vet ./... && go test ./...'

sh plugins/agent-tasks/tests/session-context-test.sh  # session hook; needs curl, git, python3
```

## Status

Early version. The server, web UI, skills and hook work end to end, but the API and the
contract may still change. Issues and feedback are welcome.

## License

[MIT](LICENSE)
