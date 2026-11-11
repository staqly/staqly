#!/usr/bin/env bash
# Runs one shard of apps/api's `go test -race` (ci.yml): "other packages" is every package but internal/httpapi,
# "httpapi n/m" every m-th top-level test of internal/httpapi (sorted), starting with the n-th. Run in apps/api.
set -euo pipefail

shard=$1
if [ "$shard" = "other packages" ]; then
  pkgs=()
  while IFS= read -r p; do pkgs+=("$p"); done < <(go list ./... | grep -v '/internal/httpapi$')
  exec go test -race -timeout 15m "${pkgs[@]}"
fi
[[ $shard =~ ^httpapi\ ([0-9]+)/([0-9]+)$ ]] || { echo "unknown shard: $shard" >&2; exit 2; }
n=${BASH_REMATCH[1]} m=${BASH_REMATCH[2]}
tests=()
while IFS= read -r t; do tests+=("$t"); done < <(grep -hoE '^func Test[A-Za-z0-9_]+\(t \*testing\.T\)' internal/httpapi/*_test.go | sed -E 's/^func (Test[A-Za-z0-9_]+).*/\1/' | sort -u)
mine=()
for i in "${!tests[@]}"; do
  if (( i % m == n - 1 )); then mine+=("${tests[$i]}"); fi
done
echo "${#mine[@]} of ${#tests[@]} tests"
exec go test -race -timeout 15m -run "^($(IFS='|'; echo "${mine[*]}"))\$" ./internal/httpapi
