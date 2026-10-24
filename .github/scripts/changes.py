"""Select CI jobs from the whole push or PR diff; uncertainty runs the full suite."""
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
    'docs': r'^(apps/docs/|packages/|apps/api/openapi\.yaml$)',
    'web': r'^(apps/web/|packages/)',
    'packages': r'^packages/',
    'sdk_ts': r'^(sdks/typescript/|apps/api/openapi\.yaml$)',
    'sdk_py': r'^sdks/python/',
    # The image embeds the dashboard, so frontend/shared changes need the Docker check too.
    'e2e': r'^(apps/(api|cli|platform)/|packages/|sdks/|scripts/|e2e/|Dockerfile$|\.dockerignore$|compose)',
}


def changed_files(event, payload):
    if event == 'pull_request':
        base = 'HEAD^1'  # checkout's merge commit: first parent is the PR base
    elif event == 'push':
        base = payload.get('before', '')
        if not re.fullmatch(r'[0-9a-f]{40}', base) or base == '0' * 40:
            return None
    else:
        return None
    try:
        return subprocess.check_output(
            ['git', 'diff', '--name-only', '--no-renames', '-z', base, 'HEAD', '--'],
            stderr=subprocess.DEVNULL,
        ).decode().rstrip('\0').split('\0')
    except subprocess.CalledProcessError:
        return None  # a missing/rewritten base must never silently skip checks


def selected_jobs(files):
    if files is None or any(re.search(COMMON, path) for path in files):
        return set(JOBS)
    return {job for job, pattern in JOBS.items() if any(re.search(pattern, path) for path in files)}


if __name__ == '__main__':
    payload = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
    selected = selected_jobs(changed_files(os.environ['GITHUB_EVENT_NAME'], payload))
    output = '\n'.join(f'{job}={str(job in selected).lower()}' for job in JOBS) + '\n'
    with open(os.environ['GITHUB_OUTPUT'], 'a') as destination:
        destination.write(output)
    print(output, end='')
