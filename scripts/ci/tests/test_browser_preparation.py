"""Execute the workflow's actual parallel orchestration with harmless command stubs."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


WORKFLOW = Path(__file__).resolve().parents[3] / ".github/workflows/ci.yml"


class BrowserPreparationTests(unittest.TestCase):
    def run_preparation(self, failure):
        import textwrap
        workflow = WORKFLOW.read_text()
        block = workflow.split("      - name: Prepare frontend alongside the complete historical import\n", 1)[1]
        block = block.split("        run: |\n", 1)[1].split("\n      - name:", 1)[0]
        code = textwrap.dedent(block)
        prefix = code.split('runtime="$RUNNER_TEMP/glean-browser"', 1)[0]
        suffix = code[code.rindex('wait "$frontend_pid"'):]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "web").mkdir()
            (root / "bin").mkdir()
            for name in ("npm", "npx"):
                command = root / "bin" / name
                command.write_text('#!/bin/bash\nset -eu\nsleep 0.05\nif [[ "$FAILURE" == "$1" ]]; then exit 13; fi\necho "$1" >> "$TRACE"\n')
                command.chmod(0o700)
            trace = root / "trace"
            main = 'if [[ "$FAILURE" == import ]]; then exit 17; fi\necho import >> "$TRACE"\n'
            result = subprocess.run(["bash", "-c", prefix + main + suffix + '\necho acceptance >> "$TRACE"\n'],
                                    cwd=root, env={**os.environ, "PATH": str(root / "bin") + os.pathsep + os.environ["PATH"],
                                                   "FAILURE": failure, "TRACE": str(trace)},
                                    capture_output=True, text=True, timeout=5)
            return result.returncode, trace.read_text().splitlines() if trace.exists() else []

    def test_acceptance_waits_for_all_preparation(self):
        status, trace = self.run_preparation("")
        self.assertEqual(status, 0)
        self.assertEqual(trace[-1], "acceptance")
        self.assertEqual(set(trace), {"ci", "run", "playwright", "import", "acceptance"})

    def test_each_frontend_failure_stops_acceptance(self):
        for command in ("ci", "run", "playwright"):
            with self.subTest(command=command):
                status, trace = self.run_preparation(command)
                self.assertNotEqual(status, 0)
                self.assertNotIn("acceptance", trace)

    def test_import_failure_stops_acceptance_and_reaps_frontend(self):
        status, trace = self.run_preparation("import")
        self.assertEqual(status, 17)
        self.assertIn("playwright", trace)
        self.assertNotIn("acceptance", trace)
