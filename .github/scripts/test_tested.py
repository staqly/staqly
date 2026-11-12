"""The "already tested" skip (.github/actions/tested/tested.sh), with a stub `gh` and a throwaway repository."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / 'actions' / 'tested' / 'tested.sh'
# Stands in for `gh api <endpoint> -q <jq>`: the artifact list names run 101; the run is $RUN_JSON.
GH = '''#!/bin/sh
case "$2" in
  *actions/artifacts*) echo 101 ;;
  *actions/runs/101) printf '%s' "$RUN_JSON" | jq -r "$4" ;;
  *) exit 1 ;;
esac
'''


def skip(run):
    with tempfile.TemporaryDirectory() as directory:
        gh = Path(directory, 'bin', 'gh')
        gh.parent.mkdir()
        gh.write_text(GH)
        gh.chmod(0o755)
        repo = Path(directory, 'repo')
        repo.mkdir()
        for args in (['init', '-q'], ['-c', 'user.name=Test', '-c', 'user.email=test@example.com',
                                       'commit', '-q', '--allow-empty', '-m', 'one']):
            subprocess.check_call(['git', *args], cwd=repo)
        output = Path(directory, 'output')
        subprocess.check_call(['bash', str(SCRIPT)], cwd=repo, stdout=subprocess.DEVNULL, env={
            **os.environ, 'PATH': f'{gh.parent}:{os.environ["PATH"]}', 'RUN_JSON': json.dumps(run),
            'EVENT': 'push', 'REPO': 'staqly/staqly', 'SHA': '0' * 40, 'RUNNER_TEMP': directory,
            'WORKFLOW_REF': 'staqly/staqly/.github/workflows/ci.yml@refs/heads/main', 'GITHUB_OUTPUT': str(output),
        })
        return output.read_text().strip()


class TestedTest(unittest.TestCase):
    RUN = {'conclusion': 'success', 'path': '.github/workflows/ci.yml', 'event': 'pull_request',
           'head_repository': {'full_name': 'staqly/staqly'}}

    def test_a_passed_pull_request_run_of_this_repository_counts(self):
        self.assertEqual(skip(self.RUN), 'skip=true')

    def test_a_failed_run_or_another_workflow_does_not_count(self):
        self.assertEqual(skip({**self.RUN, 'conclusion': 'failure'}), 'skip=false')
        self.assertEqual(skip({**self.RUN, 'path': '.github/workflows/other.yml'}), 'skip=false')

    # A fork's pull request runs the fork's copy of the workflow: it can pass trivially and upload an artifact
    # named for any tree, e.g. the one main will have after someone else's pull request is merged.
    def test_a_run_from_a_fork_does_not_count(self):
        self.assertEqual(skip({**self.RUN, 'head_repository': {'full_name': 'someone/staqly'}}), 'skip=false')
        self.assertEqual(skip({**self.RUN, 'head_repository': None}), 'skip=false')


if __name__ == '__main__':
    unittest.main()
