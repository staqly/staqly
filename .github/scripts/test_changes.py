import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from changes import changed_files, selected_jobs


class ChangesTest(unittest.TestCase):
    def test_frontend_keeps_backend_jobs_off(self):
        self.assertEqual(selected_jobs(['apps/platform/components/ui/calendar.tsx']),
                         {'dashboard', 'e2e'})
        self.assertEqual(selected_jobs(['apps/web/app/page.tsx']), {'web'})

    def test_shared_inputs_and_full_run(self):
        all_jobs = selected_jobs(None)
        for path in ['.github/workflows/ci.yml', 'pnpm-lock.yaml', 'turbo.json', '.nvmrc']:
            self.assertEqual(selected_jobs([path]), all_jobs)
        self.assertEqual(selected_jobs([]), set())

    def test_api_schema_and_brand_dependents(self):
        self.assertEqual(selected_jobs(['apps/api/openapi.yaml']),
                         {'api', 'cli', 'dashboard', 'docs', 'sdk_ts', 'e2e'})
        self.assertEqual(selected_jobs(['packages/brand/logo.tsx']),
                         {'dashboard', 'docs', 'web', 'packages', 'e2e'})

    def test_unknown_base_runs_everything(self):
        self.assertIsNone(changed_files('push', {'before': '0' * 40}))
        self.assertIsNone(changed_files('push', {'before': 'f' * 40}))
        self.assertIsNone(changed_files('schedule', {}))
        self.assertIsNone(changed_files('workflow_dispatch', {}))

    def test_push_covers_every_commit_and_both_sides_of_rename(self):
        with tempfile.TemporaryDirectory() as directory:
            previous = Path.cwd()
            os.chdir(directory)
            try:
                def git(*args):
                    return subprocess.check_output(['git', *args], text=True, stderr=subprocess.DEVNULL).strip()

                git('init')
                git('config', 'user.name', 'Test')
                git('config', 'user.email', 'test@example.com')
                Path('old').write_text('original')
                git('add', '.')
                git('commit', '-m', 'base')
                base = git('rev-parse', 'HEAD')
                Path('first').write_text('first commit')
                git('add', '.')
                git('commit', '-m', 'first')
                git('mv', 'old', 'new')
                git('commit', '-m', 'second')
                self.assertEqual(set(changed_files('push', {'before': base})), {'first', 'old', 'new'})
                self.assertEqual(set(changed_files('pull_request', {})), {'old', 'new'})
            finally:
                os.chdir(previous)


if __name__ == '__main__':
    unittest.main()
