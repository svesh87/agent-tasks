#!/bin/sh
# Self-test for scripts/session-context.sh against a stub server (python3 http.server).
# Run: sh plugins/agent-tasks/tests/session-context-test.sh   (needs sh, git, curl, python3)
set -u

here=$(cd "$(dirname "$0")" && pwd)
hook=$here/../scripts/session-context.sh
tmp=$(mktemp -d)
pid=
cleanup() {
	[ -n "$pid" ] && kill "$pid" 2>/dev/null
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

fails=0
check() { # name expected actual
	if [ "$2" = "$3" ]; then
		echo "ok   $1"
	else
		echo "FAIL $1"
		echo "     expected: $2"
		echo "     actual:   $3"
		fails=$((fails + 1))
	fi
}

# --- remote normalisation
n() { sh "$hook" --normalize "$1"; }
check "scp form" "github.com/owner/repo" "$(n git@github.com:owner/repo.git)"
check "https" "github.com/owner/repo" "$(n https://github.com/owner/repo.git)"
check "https creds+port" "gitlab.example.com/group/sub/repo" "$(n https://user:secret@GitLab.Example.com:8443/group/sub/repo/)"
check "ssh scheme" "host.example/a/b" "$(n ssh://git@host.example:2222/a/b.git)"
check "no .git" "github.com/o/r" "$(n https://github.com/o/r)"

# --- stub server: checks the token, echoes the query; /slow sleeps, /fail answers 500
cat >"$tmp/stub.py" <<'EOF'
import http.server, sys, time, urllib.parse
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(u.query, keep_blank_values=True)
        if self.headers.get("Authorization") != "Bearer test-token":
            self.send_response(401); self.end_headers(); return
        # /proxy/...: an auth proxy that also wants two service-token headers.
        if u.path.startswith("/proxy/") and (self.headers.get("CF-Access-Client-Id") != "id-1"
                or self.headers.get("CF-Access-Client-Secret") != 'se"c\\ret=1'):
            self.send_response(403); self.end_headers(); return
        if u.path.startswith("/slow/"):
            time.sleep(4)
        if u.path.startswith("/fail/"):
            self.send_response(500); self.end_headers(); self.wfile.write(b"boom"); return
        if q.get("repo", [""])[0] == "github.com/none/empty":
            self.send_response(200); self.end_headers(); return
        body = "agent-tasks: place [%s]\nmachine=%s cwd=%s\n" % (q.get("repo", [""])[0], q.get("machine", [""])[0], q.get("cwd", [""])[0])
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(body.encode())
s = http.server.HTTPServer(("127.0.0.1", 0), H)
print(s.server_address[1], flush=True)
s.serve_forever()
EOF
python3 "$tmp/stub.py" >"$tmp/port" 2>/dev/null &
pid=$!
i=0
while [ ! -s "$tmp/port" ] && [ $i -lt 50 ]; do
	sleep 0.1
	i=$((i + 1))
done
port=$(cat "$tmp/port")
base=http://127.0.0.1:$port

# --- a repository with an origin remote, and a plain directory
repo=$tmp/work/repo
mkdir -p "$repo/sub" "$tmp/plain"
git -C "$repo" init -q
git -C "$repo" remote add origin git@github.com:Owner/Repo.git

run() { # cwd, then env assignments; feeds hook-style JSON on stdin
	d=$1
	shift
	printf '{"session_id":"x","cwd":"%s","hook_event_name":"SessionStart","source":"startup"}' "$d" |
		env -i PATH="$PATH" HOME="$tmp/home" "$@" sh "$hook"
	echo "exit=$?"
}

out=$(run "$repo/sub" AGENT_TASKS_URL="$base" AGENT_TASKS_TOKEN=test-token AGENT_TASKS_MACHINE=ci-runner.lan)
check "repo from subdir, explicit machine kept" "agent-tasks: place [github.com/Owner/Repo]
machine=ci-runner.lan cwd=$repo
exit=0" "$out"

short=$(hostname 2>/dev/null || uname -n)
short=${short%%.*}
out=$(run "$repo" AGENT_TASKS_URL="$base" AGENT_TASKS_TOKEN=test-token)
check "machine defaults to short host name" "agent-tasks: place [github.com/Owner/Repo]
machine=$short cwd=$repo
exit=0" "$out"

out=$(run "$tmp/plain" AGENT_TASKS_URL="$base/" AGENT_TASKS_TOKEN=test-token AGENT_TASKS_MACHINE=box)
check "no git: cwd only" "agent-tasks: place []
machine=box cwd=$tmp/plain
exit=0" "$out"

mkdir -p "$tmp/home/.config/agent-tasks"
printf '# test\nurl = %s\ntoken=test-token\nmachine = cfgbox\n' "$base" >"$tmp/home/.config/agent-tasks/config"
out=$(run "$repo")
check "config file" "agent-tasks: place [github.com/Owner/Repo]
machine=cfgbox cwd=$repo
exit=0" "$out"
rm "$tmp/home/.config/agent-tasks/config"

check "no settings" "exit=0" "$(run "$repo")"
check "wrong token" "exit=0" "$(run "$repo" AGENT_TASKS_URL="$base" AGENT_TASKS_TOKEN=nope)"
check "server error" "exit=0" "$(run "$repo" AGENT_TASKS_URL="$base/fail" AGENT_TASKS_TOKEN=test-token)"
check "server down" "exit=0" "$(run "$repo" AGENT_TASKS_URL=http://127.0.0.1:9 AGENT_TASKS_TOKEN=test-token)"

start=$(date +%s)
out=$(run "$repo" AGENT_TASKS_URL="$base/slow" AGENT_TASKS_TOKEN=test-token)
took=$(($(date +%s) - start))
check "slow server" "exit=0" "$out"
[ "$took" -le 3 ] && echo "ok   slow server gives up in ${took}s" || {
	echo "FAIL slow server took ${took}s"
	fails=$((fails + 1))
}

# --- extra headers for an auth proxy (value with a quote, a backslash and '=')
secret='se"c\ret=1'
want="agent-tasks: place [github.com/Owner/Repo]
machine=px cwd=$repo
exit=0"
check "proxy: headers missing" "exit=0" "$(run "$repo" AGENT_TASKS_URL="$base/proxy" AGENT_TASKS_TOKEN=test-token AGENT_TASKS_MACHINE=px)"
check "proxy: headers from env" "$want" "$(run "$repo" AGENT_TASKS_URL="$base/proxy" AGENT_TASKS_TOKEN=test-token AGENT_TASKS_MACHINE=px \
	AGENT_TASKS_HEADERS="CF-Access-Client-Id: id-1
CF-Access-Client-Secret: $secret")"
printf '# proxy service token\nCF-Access-Client-Id: id-1\nCF-Access-Client-Secret: %s\n' "$secret" >"$tmp/headers"
check "proxy: headers file" "$want" "$(run "$repo" AGENT_TASKS_URL="$base/proxy" AGENT_TASKS_TOKEN=test-token AGENT_TASKS_MACHINE=px AGENT_TASKS_HEADERS_FILE="$tmp/headers")"
printf 'url = %s/proxy\ntoken = test-token\nmachine = px\nheader = CF-Access-Client-Id: id-1\nheader = CF-Access-Client-Secret: %s\n' "$base" "$secret" >"$tmp/home/.config/agent-tasks/config"
check "proxy: headers in config" "$want" "$(run "$repo")"
rm "$tmp/home/.config/agent-tasks/config"

# Secrets never reach curl's command line: a wrapper records curl's arguments.
mkdir -p "$tmp/bin"
real_curl=$(command -v curl)
printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s/argv"\nexec "%s" "$@"\n' "$tmp" "$real_curl" >"$tmp/bin/curl"
chmod +x "$tmp/bin/curl"
printf '{"cwd":"%s"}' "$repo" | env -i PATH="$tmp/bin:$PATH" HOME="$tmp/home" AGENT_TASKS_URL="$base/proxy" AGENT_TASKS_TOKEN=test-token \
	AGENT_TASKS_HEADERS_FILE="$tmp/headers" sh "$hook" >/dev/null
if [ -s "$tmp/argv" ] && ! grep -q -e test-token -e 'se"c' -e id-1 "$tmp/argv"; then
	echo "ok   no secrets in curl's arguments"
else
	echo "FAIL secrets in curl's arguments (or curl not called)"
	fails=$((fails + 1))
fi

git -C "$repo" remote set-url origin https://github.com/none/empty
check "empty answer" "exit=0" "$(run "$repo" AGENT_TASKS_URL="$base" AGENT_TASKS_TOKEN=test-token)"

if [ "$fails" -eq 0 ]; then
	echo "all passed"
else
	echo "$fails failed"
	exit 1
fi
