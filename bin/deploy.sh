#!/usr/bin/env bash
#
# Deploy: tag what is on origin/main as prod-<UTC timestamp> and push the tag.
# The tag triggers .github/workflows/release.yml, which builds the images and
# pushes them to ghcr.io/staqly/staqly-{api,web} as :prod-<timestamp> and :latest.
#
# Usage:
#   bin/deploy.sh            (or: make deploy)
#
# Pin a server to one deploy: STAQLY_VERSION=prod-20261013-140500 make up

set -euo pipefail
cd "$(dirname "$0")/.."

tag="prod-$(date -u +%Y%m%d-%H%M%S)"

# Always the pushed main, never local uncommitted or unpushed work.
git fetch --quiet origin main

# Only ship a commit whose CI run on main went green.
sha=$(git rev-parse origin/main)
ci=$(gh run list --workflow ci.yml --branch main --commit "$sha" --json conclusion --jq '.[0].conclusion' 2>/dev/null || true)
if [ "$ci" != "success" ]; then
  echo "CI for ${sha:0:7} is '${ci:-not found}', not 'success'; not deploying." >&2
  exit 1
fi

git tag -a "$tag" origin/main -m "deploy $tag"
git push --quiet origin "$tag"

echo "Tagged $tag at $(git log -1 --format='%h %s' origin/main)"
echo "Images build here: https://github.com/staqly/staqly/actions/workflows/release.yml"
