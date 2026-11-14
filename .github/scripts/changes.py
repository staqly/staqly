"""Select CI jobs from a pull request's diff; pushes to main and uncertainty run the full suite.

Nothing runs when this exact code already passed CI (ALREADY_TESTED=true, from .github/actions/tested), except
on a nightly the checks whose answer changes without a commit.
"""
import json
import os
from pathlib import Path
import re
import subprocess


COMMON = r'^(\.github/|package\.json$|pnpm-lock\.yaml$|pnpm-workspace\.yaml$|turbo\.json$|\.nvmrc$|Makefile$)'
JOBS = {
    'api': r'^apps/api/',
    'cli': r'^apps/(api|cli)/',
    'dashboard': r'^(apps/platform/|packages/|apps/api/openapi\.yaml$)',
    'admin': r'^(apps/admin/|packages/)',
    'docs': r'^(apps/docs/|packages/|apps/api/openapi\.yaml$)',
    'web': r'^(apps/web/|packages/)',
    'packages': r'^packages/',
    'sdk_ts': r'^(sdks/typescript/|apps/api/openapi\.yaml$)',
    'sdk_py': r'^(sdks/python/|apps/api/openapi\.yaml$)',
    # The image embeds the dashboard and the admin panel, so frontend/shared changes need the Docker check too.
    'e2e': r'^(apps/(api|cli|platform|admin)/|packages/|sdks/|scripts/|e2e/|Dockerfile$|\.dockerignore$|compose)',
    # Our own code (gosec, Semgrep) and secrets in any of it; the workflows are scanned by the `changes` job.
    'security': r'^(apps/|packages/|sdks/|integrations/|scripts/|e2e/)',
}
# Checks against data that changes without a commit: the Go vulnerability database (govulncheck) and Semgrep's
# registry rules. A nightly runs them even on code that already passed, so a new advisory fails within a day.
FRESH = {'security'}


def changed_files(event, payload):
    # A push to main runs everything: that run is what a release checks (release.yml), so it must be complete.
    if event != 'pull_request':
        return None
    base = 'HEAD^1'  # checkout's merge commit: first parent is the PR base
    try:
        return subprocess.check_output(
            ['git', 'diff', '--name-only', '--no-renames', '-z', base, 'HEAD', '--'],
            stderr=subprocess.DEVNULL,
        ).decode().rstrip('\0').split('\0')
    except subprocess.CalledProcessError:
        return None  # a missing/rewritten base must never silently skip checks


def selected_jobs(files, already_tested=False, nightly=False):
    if already_tested:
        return set(FRESH) if nightly else set()
    if files is None or any(re.search(COMMON, path) for path in files):
        return set(JOBS)
    return {job for job, pattern in JOBS.items() if any(re.search(pattern, path) for path in files)}


if __name__ == '__main__':
    payload = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
    selected = selected_jobs(
        changed_files(os.environ['GITHUB_EVENT_NAME'], payload),
        already_tested=os.environ.get('ALREADY_TESTED') == 'true',
        nightly=os.environ['GITHUB_EVENT_NAME'] == 'schedule',
    )
    output = '\n'.join(f'{job}={str(job in selected).lower()}' for job in JOBS) + '\n'
    with open(os.environ['GITHUB_OUTPUT'], 'a') as destination:
        destination.write(output)
    print(output, end='')
