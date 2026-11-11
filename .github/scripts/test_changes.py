import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from changes import changed_files, selected_jobs


class ChangesTest(unittest.TestCase):
    def test_frontend_keeps_backend_jobs_off(self):
        self.assertEqual(selected_jobs(['apps/platform/components/ui/calendar.tsx']),
                         {'dashboard', 'e2e', 'security'})
        self.assertEqual(selected_jobs(['apps/web/app/page.tsx']), {'web', 'security'})

    def test_shared_inputs_and_full_run(self):
        all_jobs = selected_jobs(None)
        for path in ['.github/workflows/ci.yml', 'pnpm-lock.yaml', 'turbo.json', '.nvmrc']:
            self.assertEqual(selected_jobs([path]), all_jobs)
        self.assertEqual(selected_jobs([]), set())

    def test_api_schema_and_brand_dependents(self):
        self.assertEqual(selected_jobs(['apps/api/openapi.yaml']),
                         {'api', 'cli', 'dashboard', 'docs', 'sdk_ts', 'sdk_py', 'e2e', 'security'})
        self.assertEqual(selected_jobs(['packages/brand/logo.tsx']),
                         {'dashboard', 'docs', 'web', 'packages', 'e2e', 'security'})

    def test_security_scan_covers_code_not_notes(self):
        for path in ['apps/api/internal/app/app.go', 'apps/admin/app/page.tsx', 'sdks/python/staqly/client.py',
                     'integrations/n8n/workflow.json', 'Makefile']:
            self.assertIn('security', selected_jobs([path]))
        self.assertNotIn('security', selected_jobs(['README.md']))

    def test_openapi_change_runs_python_model_contract(self):
        self.assertIn('sdk_py', selected_jobs(['apps/api/openapi.yaml']))

    def test_main_and_full_runs_run_everything(self):
        self.assertIsNone(changed_files('push', {'before': '0' * 40}))
        self.assertIsNone(changed_files('push', {'before': 'f' * 40}))
        self.assertIsNone(changed_files('schedule', {}))
        self.assertIsNone(changed_files('workflow_dispatch', {}))

    def test_already_tested_code_runs_nothing(self):
        self.assertEqual(selected_jobs(None, already_tested=True), set())
        self.assertEqual(selected_jobs(['apps/api/main.go'], already_tested=True), set())
        self.assertEqual(selected_jobs(None, already_tested=False), selected_jobs(None))

    def test_nightly_rechecks_vulnerabilities_on_tested_code(self):
        self.assertEqual(selected_jobs(None, already_tested=True, nightly=True), {'security'})
        self.assertEqual(selected_jobs(None, nightly=True), selected_jobs(None))

    def test_pull_request_covers_both_sides_of_rename(self):
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
                self.assertIsNone(changed_files('push', {'before': base}))
                self.assertEqual(set(changed_files('pull_request', {})), {'old', 'new'})
            finally:
                os.chdir(previous)


if __name__ == '__main__':
    unittest.main()
