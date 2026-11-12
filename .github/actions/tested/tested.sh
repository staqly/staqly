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
      # Only a finished, successful pull request run of this very workflow file counts, and only from a branch
      # of this repository: a fork's pull request runs the fork's copy of the workflow, which could pass
      # trivially and record any tree.
      if [ "$(gh api "repos/$REPO/actions/runs/$id" -q '.conclusion + " " + .path + " " + .event + " " + .head_repository.full_name' || true)" = "success $path pull_request $REPO" ]; then
        echo "Run $id of $path passed on this exact tree ($tree): nothing to run again."
        skip=true
        break
      fi
    done
    ;;
  schedule)
    count=$(gh api "repos/$REPO/actions/workflows/$workflow.yml/runs?event=schedule&head_sha=$SHA&status=success&per_page=1" -q .total_count) || count=0
    if [[ $count =~ ^[0-9]+$ ]] && [ "$count" -gt 0 ]; then
      echo "A scheduled run of $path already passed on $SHA: no new commits since."
      skip=true
    fi
    ;;
esac

echo "skip=$skip" >>"$GITHUB_OUTPUT"
