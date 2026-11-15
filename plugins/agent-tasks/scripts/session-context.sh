#!/bin/sh
# agent-tasks SessionStart hook (Claude Code; matchers startup, resume, compact).
#
# Asks the agent-tasks server which tasks and open todos belong to the place the session
# runs in (the git repository's origin remote, else the directory) and prints the answer.
# Claude Code adds a SessionStart hook's plain stdout to the session context.
#
# Settings, environment first, then the config file
# ${AGENT_TASKS_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/agent-tasks/config}
# with `key = value` lines (`#` comments):
#   AGENT_TASKS_URL     / url      server base URL, e.g. https://tasks.example.com
#   AGENT_TASKS_TOKEN   / token    bearer token (same as for MCP)
#   AGENT_TASKS_MACHINE / machine  this machine's name (default: the short host name);
#                                  set it explicitly in containers (random host names)
#   AGENT_TASKS_HEADERS / header   extra request headers, `Name: value`, for an auth proxy
#                                  in front of the server (e.g. a service token's
#                                  CF-Access-Client-Id / CF-Access-Client-Secret). In the
#                                  environment: one header per line; in the config file:
#                                  one `header = Name: value` line each.
#   AGENT_TASKS_HEADERS_FILE       a file with one `Name: value` header per line
#                                  (`#` comments), e.g. kept by a secrets manager.
# All headers from all sources are sent. Secrets reach curl through its config on stdin,
# never on the command line, so they do not show in `ps`.
#
# Any failure (no settings, server down, slow, error status) prints nothing and exits 0:
# a session must never be blocked or polluted by this hook.
#
# POSIX sh + curl + git; no jq. `session-context.sh --normalize <remote>` prints the
# normalised remote (used by the self-test).

MAX_OUTPUT=9000 # Claude Code caps hook stdout at 10,000 characters

# git remote → host/path: no scheme, credentials, port or .git; scp form understood.
normalize_remote() {
	r=$1
	case $r in
	*://*)
		r=${r#*://}
		r=${r#*@}
		host=${r%%/*}
		rest=${r#"$host"}
		host=${host%%:*}
		r=$host$rest
		;;
	*:*)
		r=${r#*@}
		host=${r%%:*}
		r=$host/${r#*:}
		;;
	esac
	r=${r%/}
	r=${r%.git}
	host=$(printf '%s' "${r%%/*}" | tr '[:upper:]' '[:lower:]')
	case $r in
	*/*) printf '%s/%s\n' "$host" "${r#*/}" ;;
	*) printf '%s\n' "$host" ;;
	esac
}

if [ "${1:-}" = "--normalize" ]; then
	normalize_remote "$2"
	exit 0
fi

# A value for a double-quoted string in curl's config: backslash and quote escaped.
curl_quote() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

main() {
	url=${AGENT_TASKS_URL:-}
	token=${AGENT_TASKS_TOKEN:-}
	machine=${AGENT_TASKS_MACHINE:-}
	headers=${AGENT_TASKS_HEADERS:-}
	cfg=${AGENT_TASKS_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/agent-tasks/config}
	if [ -r "$cfg" ]; then
		while IFS='=' read -r k v || [ -n "$k" ]; do
			k=$(printf '%s' "$k" | tr -d ' \t\r')
			v=$(printf '%s' "$v" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
			case $k in
			url) [ -n "$url" ] || url=$v ;;
			token) [ -n "$token" ] || token=$v ;;
			machine) [ -n "$machine" ] || machine=$v ;;
			header) headers="$headers
$v" ;;
			esac
		done <"$cfg"
	fi
	if [ -n "${AGENT_TASKS_HEADERS_FILE:-}" ] && [ -r "$AGENT_TASKS_HEADERS_FILE" ]; then
		headers="$headers
$(grep -v '^[[:space:]]*#' "$AGENT_TASKS_HEADERS_FILE")"
	fi
	[ -n "$url" ] && [ -n "$token" ] || return 0
	if [ -z "$machine" ]; then
		# Short host name: "laptop", not "laptop.local" (the suffix changes with networks).
		machine=$(hostname 2>/dev/null || uname -n)
		machine=${machine%%.*}
	fi

	# The hook input (JSON on stdin) carries "cwd"; fall back to the current directory.
	input=$(cat 2>/dev/null)
	cwd=$(printf '%s' "$input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$cwd" ] && [ -d "$cwd" ] || cwd=${CLAUDE_PROJECT_DIR:-$PWD}

	repo=
	if top=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null); then
		cwd=$top
		if remote=$(git -C "$cwd" remote get-url origin 2>/dev/null) && [ -n "$remote" ]; then
			repo=$(normalize_remote "$remote")
		fi
	fi

	# The token and extra headers go to curl through its config on stdin, not the command
	# line (ps).
	body=$({
		printf 'header = "Authorization: Bearer %s"\n' "$(curl_quote "$token")"
		printf '%s\n' "$headers" | while IFS= read -r h; do
			h=$(printf '%s' "$h" | tr -d '\r' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
			case $h in
			*:*) printf 'header = "%s"\n' "$(curl_quote "$h")" ;;
			esac
		done
	} |
		curl -K - -fsS -G --max-time 2 --connect-timeout 1 \
			--data-urlencode "repo=$repo" \
			--data-urlencode "cwd=$cwd" \
			--data-urlencode "machine=$machine" \
			"${url%/}/hooks/session-context" 2>/dev/null) || return 0
	[ -n "$body" ] || return 0
	printf '%s\n' "$body" | head -c "$MAX_OUTPUT"
}

main 2>/dev/null
exit 0
