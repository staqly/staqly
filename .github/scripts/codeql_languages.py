"""CodeQL languages to analyze (codeql.yml): a pull request runs only those its diff can change; pushes to main,
the weekly scan, manual runs, a change to the analysis itself and any doubt run all of them.
"""
import json
import os
import re

from changes import changed_files


LANGUAGES = {
    # Go modules: their sources, go.mod/go.sum (dependencies).
    'go': r'(\.go$|(^|/)go\.(mod|sum)$)',
    # The JavaScript extractor also reads HTML, JSON and YAML (package.json, pnpm-lock.yaml, tsconfig.json).
    'javascript-typescript': r'\.(c|m)?[jt]sx?$|\.(html?|vue|json|ya?ml)$',
    'python': r'(\.py$|(^|/)(pyproject\.toml|uv\.lock)$)',
    'actions': r'^\.github/(workflows|actions)/',
}
# The analysis itself, its file selection, and the Makefile Go's autobuild may run.
ALL = r'^(\.github/workflows/codeql\.yml$|\.github/scripts/(codeql_languages|changes)\.py$|\.github/codeql/|Makefile$)'


def selected_languages(files):
    if files is None or any(re.search(ALL, path) for path in files):
        return list(LANGUAGES)
    return [language for language, pattern in LANGUAGES.items() if any(re.search(pattern, path) for path in files)]


if __name__ == '__main__':
    languages = selected_languages(changed_files(os.environ['GITHUB_EVENT_NAME'], {}))
    output = f'languages={json.dumps(languages)}\n'
    with open(os.environ['GITHUB_OUTPUT'], 'a') as destination:
        destination.write(output)
    print(output, end='')
