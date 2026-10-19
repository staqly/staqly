#!/usr/bin/env bash
# Real test of `staqly service install / status / logs / uninstall` on this Mac (launchd LaunchAgent).
#
#   e2e/service/run.sh                            build the CLI from this checkout and test it
#   STAQLY_BIN=/path/to/staqly e2e/service/run.sh test another binary
#
# It installs the real LaunchAgent ai.staqly.serve in ~/Library/LaunchAgents, on ports that do not
# collide with a staqly you run yourself (API 127.0.0.1:43333, dashboard 48080; API_PORT, DASH_PORT and
# BUSY_PORT change them) with a temporary data directory, and always uninstalls it at the end, also when
# interrupted. It refuses to run when a staqly service is already installed, so it never replaces yours.
# Linux (systemd --user) is covered by e2e/service/systemd.sh in a Docker container.
set -uo pipefail

[ "$(uname -s)" = Darwin ] || {
  echo "macOS only; on Linux run e2e/service/systemd.sh" >&2
  exit 2
}

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
API_PORT=${API_PORT:-43333}
DASH_PORT=${DASH_PORT:-48080}
BUSY_PORT=${BUSY_PORT:-43335}
LABEL=ai.staqly.serve
TARGET=gui/$(id -u)/$LABEL
PLIST=$HOME/Library/LaunchAgents/$LABEL.plist
LOG=$HOME/Library/Logs/staqly/staqly.log
TMP=${TMPDIR:-/tmp}
WORK=$(mktemp -d "${TMP%/}/staqly-service-test.XXXXXX")
DATA=$WORK/data
pass=0
fail=0

ok() {
  pass=$((pass + 1))
  echo "  ok   $*"
}
bad() {
  fail=$((fail + 1))
  echo "  FAIL $*"
}
# check "description" command [args...]: the command may be a function of this script.
check() {
  local what=$1
  shift
  if "$@" >/dev/null 2>&1; then ok "$what"; else bad "$what"; fi
}
# wait_until seconds command [args...]
wait_until() {
  local n=$1
  shift
  for _ in $(seq 1 "$n"); do
    "$@" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
pid() { launchctl print "$TARGET" 2>/dev/null | awk '/^\tpid = /{print $3}'; }
loaded() { launchctl print "$TARGET" >/dev/null 2>&1; }
running() { launchctl print "$TARGET" 2>/dev/null | grep -q 'state = running'; }
code() { curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$1"; }
healthy() { [ "$(code "http://127.0.0.1:$API_PORT/health")" = 200 ]; }
running_ok() { running && healthy; }
new_pid() { # running with another pid than $1, and answering
  local p
  p=$(pid)
  [ -n "$p" ] && [ "$p" != "$1" ] && healthy
}
newlog() { tail -c +"$((LOG_START + 1))" "$LOG" 2>/dev/null; }
log_has() { newlog | grep -q "$1"; }
install() { "$BIN" service install --data "$DATA" --port "$API_PORT" --dashboard-port "$DASH_PORT" "$@"; }
status_has() { "$BIN" service status | grep -q "$1"; }
one_job() { [ "$(launchctl list | grep -c "$LABEL")" = 1 ]; }
dashboard_local_only() { lsof -nP -iTCP:"$DASH_PORT" -sTCP:LISTEN | grep -q "127.0.0.1:$DASH_PORT"; }
plist_has() { grep -q "$1" "$PLIST"; }

if [ -e "$PLIST" ] || loaded; then
  echo "A staqly service is already installed ($PLIST); not touching it. Run \`staqly service uninstall\` first." >&2
  exit 2
fi
for p in "$API_PORT" "$DASH_PORT" "$BUSY_PORT"; do
  if lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Port $p is in use; choose others with API_PORT / DASH_PORT / BUSY_PORT." >&2
    exit 2
  fi
done

BEFORE_AGENTS=$(ls "$HOME/Library/LaunchAgents" 2>/dev/null | grep -i staqly)
BEFORE_JOBS=$(launchctl list | grep -i staqly)
LOG_EXISTED=0
[ -e "$LOG" ] && LOG_EXISTED=1
BUSY_PID=
HOLD_PID=

cleanup() {
  [ -n "$BUSY_PID" ] && kill "$BUSY_PID" 2>/dev/null
  [ -n "$HOLD_PID" ] && kill "$HOLD_PID" 2>/dev/null
  [ -n "${BIN:-}" ] && "$BIN" service uninstall >/dev/null 2>&1
  launchctl bootout "$TARGET" >/dev/null 2>&1
  rm -f "$PLIST"
  # The log folder goes only if this run created it.
  [ "$LOG_EXISTED" = 0 ] && rm -rf "$HOME/Library/Logs/staqly"
  rm -rf "$WORK"
  echo "launchd after cleanup:"
  echo "  ~/Library/LaunchAgents: $(ls "$HOME/Library/LaunchAgents" 2>/dev/null | grep -i staqly || echo none) (before: ${BEFORE_AGENTS:-none})"
  echo "  launchctl list: $(launchctl list | grep -i staqly || echo none) (before: ${BEFORE_JOBS:-none})"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

if [ -n "${STAQLY_BIN:-}" ]; then
  BIN=$STAQLY_BIN
else
  echo "Building the CLI (make -C apps/cli build)..."
  make -s -C "$ROOT/apps/cli" build >/dev/null || exit 1
  BIN=$ROOT/apps/cli/bin/staqly
fi
LOG_START=0
[ -e "$LOG" ] && LOG_START=$(wc -c <"$LOG" | tr -d ' ')

echo "1. install (API 127.0.0.1:$API_PORT, dashboard $DASH_PORT, data $DATA)"
check "install exits 0" install
check "plist at $PLIST" test -f "$PLIST"
check "plist readable by its owner only (0600)" test "$(stat -f %Lp "$PLIST")" = 600
check "plist is valid (plutil -lint)" plutil -lint "$PLIST"
check "plist runs this binary" plist_has "<string>$BIN</string>"
check "plist has the API port" plist_has "<string>$API_PORT</string>"
check "plist has the data directory" plist_has "<string>$DATA</string>"
check "plist keeps PATH (AI CLI for summaries)" plist_has '<key>PATH</key>'
check "plist has KeepAlive" plist_has '<key>KeepAlive</key><true/>'
check "plist has RunAtLoad" plist_has '<key>RunAtLoad</key><true/>'
check "plist logs to $LOG" plist_has "<string>$LOG</string>"
check "launchd job running" running
check "listed by launchctl list" one_job
check "API /health 200 on 127.0.0.1:$API_PORT" healthy
if [ -e "$ROOT/apps/api/web/index.html" ] || [ -n "${STAQLY_BIN:-}" ]; then
  check "dashboard 200 on 127.0.0.1:$DASH_PORT" test "$(code "http://127.0.0.1:$DASH_PORT/")" = 200
else
  check "dashboard answers on $DASH_PORT (503: not built into this binary, run make dashboard)" test "$(code "http://127.0.0.1:$DASH_PORT/")" = 503
fi
check "dashboard on 127.0.0.1 only (no --expose)" dashboard_local_only
check "data directory used" test -f "$DATA/workspace.db"
check "log says where it listens" log_has "addr=127.0.0.1:$API_PORT"
if command -v claude >/dev/null 2>&1 || command -v codex >/dev/null 2>&1; then
  check "service found the AI CLI on the captured PATH" log_has 'summaries and tags.*command='
fi
check "service status: running and answering" status_has "answers on http://127.0.0.1:$API_PORT"
check "service logs prints the log" sh -c "'$BIN' service logs | grep -q 'addr=127.0.0.1:$API_PORT'"

echo "2. restart and crash"
P1=$(pid)
launchctl kickstart -k "$TARGET"
wait_until 15 new_pid "$P1"
check "launchctl kickstart -k restarts it (pid $P1 -> $(pid)) and it answers" new_pid "$P1"
P2=$(pid)
kill -9 "$P2"
T0=$(date +%s)
wait_until 30 new_pid "$P2"
check "KeepAlive restarts a killed process (pid $P2 -> $(pid) in $(($(date +%s) - T0))s)" new_pid "$P2"

echo "3. reinstall (upgrade) is idempotent"
# Regression: a request in flight slows the graceful shutdown, launchctl bootout returned before the old
# process had exited, and the new bootstrap failed ("5: Input/output error"), leaving no service loaded.
(
  printf 'GET /health HTTP/1.1\r\nHost: 127.0.0.1\r\n'
  sleep 15
) | nc 127.0.0.1 "$API_PORT" >/dev/null 2>&1 &
HOLD_PID=$!
sleep 1
check "reinstall while a request is in flight" install
kill "$HOLD_PID" 2>/dev/null
HOLD_PID=
check "running and answering after it" running_ok
check "reinstall #2" install
check "reinstall #3" install
check "still one job, running and answering" sh -c "[ \$(launchctl list | grep -c $LABEL) = 1 ] && curl -sf http://127.0.0.1:$API_PORT/health"
check "status after reinstalls" "$BIN" service status

echo "4. busy port"
python3 -m http.server "$BUSY_PORT" --bind 127.0.0.1 --directory "$WORK" >/dev/null 2>&1 &
BUSY_PID=$!
wait_until 10 lsof -nP -iTCP:"$BUSY_PORT" -sTCP:LISTEN
OUT=$("$BIN" service install --data "$DATA" --port "$BUSY_PORT" --dashboard-port "$DASH_PORT" 2>&1)
RC=$?
check "install on a busy port fails (exit $RC)" test "$RC" != 0
check "and says the port is in use" grep -q "port $BUSY_PORT is already in use" <<<"$OUT"
check "the previous service is back and answering" wait_until 15 running_ok
kill "$BUSY_PID" 2>/dev/null
BUSY_PID=

echo "5. uninstall"
check "uninstall exits 0" "$BIN" service uninstall
check "plist removed" test ! -e "$PLIST"
check "launchd job gone" sh -c "! launchctl print $TARGET"
check "not in launchctl list" sh -c "! launchctl list | grep -q $LABEL"
check "API port closed" sh -c "! curl -s --max-time 2 http://127.0.0.1:$API_PORT/health"
check "data kept" test -f "$DATA/workspace.db"
check "log kept" test -f "$LOG"
OUT=$("$BIN" service status 2>&1)
RC=$?
check "status: not installed (exit $RC)" test "$RC" = 1
check "status says so" grep -q 'Not installed' <<<"$OUT"
check "uninstall again is harmless" "$BIN" service uninstall

echo "6. install again"
check "re-install exits 0" install
check "running and answering" running_ok
check "final uninstall" "$BIN" service uninstall
check "nothing left loaded" sh -c "! launchctl print $TARGET && test ! -e '$PLIST'"

echo
echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
