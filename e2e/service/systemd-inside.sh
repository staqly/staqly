#!/bin/bash
# Runs inside the systemd container (e2e/service/systemd.sh) as user u: `staqly service` against a real
# systemd user manager.
set -uo pipefail
export XDG_RUNTIME_DIR=/run/user/$(id -u)
BIN=/usr/local/bin/staqly
UNIT=$HOME/.config/systemd/user/staqly.service
DATA=$HOME/staqly-data
API_PORT=43333
BUSY_PORT=43335
pass=0
fail=0

check() {
  local what=$1
  shift
  if "$@" >/dev/null 2>&1; then
    pass=$((pass + 1))
    echo "  ok   $what"
  else
    fail=$((fail + 1))
    echo "  FAIL $what"
  fi
}
wait_until() {
  local n=$1
  shift
  for _ in $(seq 1 "$n"); do
    "$@" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}
healthy() { [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$API_PORT/health")" = 200 ]; }
active() { [ "$(systemctl --user is-active staqly.service)" = active ]; }
mainpid() { systemctl --user show -p MainPID --value staqly.service; }
new_pid() {
  local p
  p=$(mainpid)
  [ -n "$p" ] && [ "$p" != 0 ] && [ "$p" != "$1" ] && healthy
}
env_of_service() { tr '\0' '\n' <"/proc/$(mainpid)/environ" | grep -qxF "$1"; }
install() { "$BIN" service install --data "$DATA" --port "$API_PORT" --dashboard-port 48080; }

echo "1. install"
# Values with characters systemd treats specially in unit files: "$" (expanded in ExecStart=, literal in
# Environment=), "%" (specifiers) and quotes.
export STAQLY_TEST_SECRET='pa$word 100% "q"'
OUT=$(install 2>&1)
RC=$?
echo "$OUT" | sed 's/^/     | /'
check "install exits 0" test "$RC" = 0
check "unit at $UNIT" test -f "$UNIT"
check "unit readable by its owner only (0600)" test "$(stat -c %a "$UNIT")" = 600
check "systemd parses the unit (systemd-analyze verify)" systemd-analyze --user verify "$UNIT"
check "enabled" sh -c "[ \$(systemctl --user is-enabled staqly.service) = enabled ]"
check "active" active
check "lingering on (starts at boot)" sh -c "loginctl show-user u -p Linger | grep -q yes"
check "API /health 200" healthy
check "data directory used" test -f "$DATA/workspace.db"
check "PATH reaches the service" env_of_service "PATH=$PATH"
check "a value with \$, % and quotes reaches the service unchanged" env_of_service "STAQLY_TEST_SECRET=$STAQLY_TEST_SECRET"
check "service status: running and answering" sh -c "'$BIN' service status | grep -q 'answers on http://127.0.0.1:$API_PORT'"
check "service logs (journalctl) shows it listening" sh -c "'$BIN' service logs | grep -q 'addr=127.0.0.1:$API_PORT'"

echo "2. restart and crash"
P1=$(mainpid)
systemctl --user restart staqly.service
wait_until 15 new_pid "$P1"
check "systemctl --user restart (pid $P1 -> $(mainpid))" new_pid "$P1"
P2=$(mainpid)
kill -9 "$P2"
wait_until 30 new_pid "$P2"
check "Restart=always restarts a killed process (pid $P2 -> $(mainpid))" new_pid "$P2"

echo "3. reinstall"
check "reinstall #1" install
check "reinstall #2" install
check "active and answering" sh -c "[ \$(systemctl --user is-active staqly.service) = active ] && curl -sf http://127.0.0.1:$API_PORT/health"

echo "4. busy port"
nc -l 127.0.0.1 "$BUSY_PORT" >/dev/null 2>&1 &
NC=$!
sleep 1
OUT=$("$BIN" service install --data "$DATA" --port "$BUSY_PORT" --dashboard-port 48080 2>&1)
RC=$?
check "install on a busy port fails (exit $RC)" test "$RC" != 0
check "and says the port is in use" grep -q "port $BUSY_PORT is already in use" <<<"$OUT"
check "the previous service is back and answering" wait_until 15 healthy
kill "$NC" 2>/dev/null

echo "5. uninstall"
check "uninstall exits 0" "$BIN" service uninstall
check "unit removed" test ! -e "$UNIT"
check "not active" sh -c "! systemctl --user is-active staqly.service"
check "API port closed" sh -c "! curl -s --max-time 2 http://127.0.0.1:$API_PORT/health"
check "data kept" test -f "$DATA/workspace.db"
OUT=$("$BIN" service status 2>&1)
RC=$?
check "status: not installed (exit $RC)" test "$RC" = 1
check "status says so" grep -q 'Not installed' <<<"$OUT"
check "re-install works" install
check "final uninstall" "$BIN" service uninstall

echo
echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
