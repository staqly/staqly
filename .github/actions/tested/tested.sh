#!/usr/bin/env bash
# See action.yml. Prints why it skips; anything it cannot prove leaves skip=false.
set -euo pipefail

path=${WORKFLOW_REF#*/*/} # owner/repo/.github/workflows/ci.yml@ref -> .github/workflows/ci.yml@ref
path=${path%%@*}
workflow=$(basename "$path" .yml)
tree=$(git rev-parse 'HEAD^{tree}')
name="tested-$workflow-$tree"
skip=false

case "$EVENT" in
  pull_request)
    printf '%s\n' "$SHA" >"$RUNNER_TEMP/$name"
    echo "artifact=$name" >>"$GITHUB_OUTPUT"
    echo "file=$RUNNER_TEMP/$name" >>"$GITHUB_OUTPUT"
    ;;
  push)
    runs=$(gh api "repos/$REPO/actions/artifacts?name=$name&per_page=30" -q '.artifacts[].workflow_run.id') || runs=""
    for id in $runs; do
      [[ $id =~ ^[0-9]+$ ]] || continue # never an error message read as run ids
      # Only a finished, successful run of this very workflow file counts.
      if [ "$(gh api "repos/$REPO/actions/runs/$id" -q '.conclusion + " " + .path' || true)" = "success $path" ]; then
        echo "Run $id of $path passed on this exact tree ($tree): nothing to run again."
        skip=true
        break
      fi
    done
    ;;
esac

echo "skip=$skip" >>"$GITHUB_OUTPUT"
