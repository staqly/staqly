#!/usr/bin/env bash
#
# Release: tag what is on origin/main as v<semver> and push the tag. The tag
# triggers .github/workflows/release.yml, which publishes that version as
# the staqly binaries (GitHub release, installed by scripts/install.sh) and
# the image ghcr.io/staqly/staqly:v<semver> (+ :latest).
#
# Usage:
#   bin/release.sh v0.2.0      (or: make release VERSION=v0.2.0)

set -euo pipefail
cd "$(dirname "$0")/.."

tag="${1:-}"
if ! [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: bin/release.sh v<major>.<minor>.<patch>   (got '${tag}')" >&2
  exit 2
fi

# Always the pushed main, never local uncommitted or unpushed work.
git fetch --quiet --tags origin main
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
  echo "tag $tag already exists" >&2
  exit 1
fi

# Only release a commit whose CI run on main went green.
sha=$(git rev-parse origin/main)
ci=$(gh run list --workflow ci.yml --branch main --commit "$sha" --json conclusion --jq '.[0].conclusion' 2>/dev/null || true)
if [ "$ci" != "success" ]; then
  echo "CI for ${sha:0:7} is '${ci:-not found}', not 'success'; not releasing." >&2
  exit 1
fi

git tag -a "$tag" origin/main -m "release $tag"
git push --quiet origin "$tag"

echo "Tagged $tag at $(git log -1 --format='%h %s' origin/main)"
echo "Binaries and images build here: https://github.com/staqly/staqly/actions/workflows/release.yml"
