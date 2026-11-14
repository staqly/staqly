#!/usr/bin/env bash
# Real test of `staqly service` on Linux: a Linux build of the CLI from this checkout runs
# e2e/service/systemd-inside.sh in a Docker container with systemd as PID 1 and a lingering user manager.
# The container (staqly-service-systemd) publishes no ports and is removed at the end.
#
#   e2e/service/systemd.sh
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
NAME=staqly-service-systemd
IMAGE=staqly-service-systemd:test
TMP=${TMPDIR:-/tmp}
WORK=$(mktemp -d "${TMP%/}/staqly-systemd.XXXXXX")

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
docker rm -f "$NAME" >/dev/null 2>&1 || true # a leftover of an interrupted run

ARCH=$(docker version -f '{{.Server.Arch}}')
echo "Building the CLI for linux/$ARCH..."
(cd "$ROOT/apps/cli" && CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath -o "$WORK/staqly" ./cmd/staqly)
docker build -q -t "$IMAGE" -f "$ROOT/e2e/service/systemd.Dockerfile" "$ROOT/e2e/service" >/dev/null
docker run -d --name "$NAME" --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw "$IMAGE" >/dev/null
for _ in $(seq 1 30); do
  docker exec "$NAME" systemctl is-active user@1000.service >/dev/null 2>&1 && break
  sleep 1
done
docker cp "$WORK/staqly" "$NAME:/usr/local/bin/staqly"
docker cp "$ROOT/e2e/service/systemd-inside.sh" "$NAME:/usr/local/bin/staqly-service-test"
docker exec -u u -w /home/u "$NAME" bash /usr/local/bin/staqly-service-test
