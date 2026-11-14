import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from changes import changed_files
from codeql_languages import LANGUAGES, selected_languages


class CodeQLLanguagesTest(unittest.TestCase):
    def test_single_language(self):
        self.assertEqual(selected_languages(['apps/api/internal/app/app.go']), ['go'])
        self.assertEqual(selected_languages(['apps/platform/components/ui/calendar.tsx']), ['javascript-typescript'])
        self.assertEqual(selected_languages(['sdks/python/staqly/client.py']), ['python'])
        self.assertEqual(selected_languages(['README.md', 'apps/docs/content/docs/index.mdx']), [])

    def test_dependencies_and_shared_files(self):
        self.assertEqual(selected_languages(['apps/cli/go.sum']), ['go'])
        self.assertEqual(selected_languages(['sdks/python/uv.lock']), ['python'])
        self.assertEqual(selected_languages(['pnpm-lock.yaml']), ['javascript-typescript'])
        self.assertEqual(selected_languages(['apps/api/go.mod', 'package.json']), ['go', 'javascript-typescript'])

    def test_ci_configuration(self):
        self.assertEqual(selected_languages(['.github/actions/setup/action.yml']),
                         ['javascript-typescript', 'actions'])
        for path in ['.github/workflows/codeql.yml', '.github/scripts/codeql_languages.py',
                     '.github/scripts/changes.py', 'Makefile']:
            self.assertEqual(selected_languages([path]), list(LANGUAGES))

    def test_unknown_changes_and_full_runs_analyze_everything(self):
        self.assertEqual(selected_languages(None), list(LANGUAGES))
        for event in ['push', 'schedule', 'workflow_dispatch']:
            self.assertEqual(selected_languages(changed_files(event, {})), list(LANGUAGES))

    def test_deleted_file_and_missing_base(self):
        with tempfile.TemporaryDirectory() as directory:
            previous = Path.cwd()
            os.chdir(directory)
            try:
                def git(*args):
                    return subprocess.check_output(['git', *args], text=True, stderr=subprocess.DEVNULL).strip()

                git('init')
                git('config', 'user.name', 'Test')
                git('config', 'user.email', 'test@example.com')
                # A single commit has no HEAD^1: detection fails, so everything runs.
                Path('worker.py').write_text('print(1)')
                git('add', '.')
                git('commit', '-m', 'base')
                self.assertEqual(selected_languages(changed_files('pull_request', {})), list(LANGUAGES))
                git('rm', '-q', 'worker.py')
                git('commit', '-m', 'delete')
                self.assertEqual(selected_languages(changed_files('pull_request', {})), ['python'])
            finally:
                os.chdir(previous)


if __name__ == '__main__':
    unittest.main()
