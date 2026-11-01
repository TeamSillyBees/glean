"""Offline hostile/missing go-test-json events; never performs source requests."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "live_canary_report.py"
spec = importlib.util.spec_from_file_location("live_canary_report", SCRIPT)
reporter = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reporter)


def events():
    return [{"Package": reporter.PACKAGE, "Test": name, "Action": "pass", "Elapsed": 0.25}
            for name in [*(reporter.TEST + "/" + s for s in reporter.SOURCES), reporter.TEST, ""]]


def summarize(items, code=0):
    return reporter.summarize([json.dumps(x) for x in items], code)


class ReportTests(unittest.TestCase):
    def test_all_sources_and_package_must_pass(self):
        text, passed = summarize(events())
        self.assertTrue(passed)
        self.assertEqual(text.count("| pass | 0.250 |"), 6)
        for missing in range(8):
            items = events()
            del items[missing]
            self.assertFalse(summarize(items)[1])
        for code in (1, 124, None):
            self.assertFalse(summarize(events(), code)[1])

    def test_fail_skip_not_run_and_package_failure(self):
        for index in range(8):
            for action in ("fail", "skip"):
                items = events()
                items[index]["Action"] = action
                self.assertFalse(summarize(items)[1])
        text, passed = summarize([])
        self.assertFalse(passed)
        self.assertEqual(text.count("| not-run |"), 6)

    def test_never_copies_output_or_unknown_fields(self):
        secret = "SECRET-header-body-token-::error::"
        items = events()
        for item in items:
            item.update(Output=secret, Time=secret, Unknown=secret)
        items += [{"Package": reporter.PACKAGE, "Test": reporter.TEST + "/" + secret,
                   "Action": "fail", "Elapsed": 1},
                  {"Package": secret, "Action": "output", "Output": secret}]
        text, passed = summarize(items)
        self.assertTrue(passed)
        self.assertNotIn(secret, text)

    def test_malformed_duplicate_and_invalid_duration_fail_closed(self):
        for elapsed in (None, True, "secret", -1, float("nan"), float("inf"), 3601, 10**500):
            items = events()
            items[0]["Elapsed"] = elapsed
            text, passed = summarize(items)
            self.assertFalse(passed)
            self.assertNotIn("secret", text)
        self.assertFalse(summarize(events() + events()[:1])[1])
        for field in ("Action", "Test"):
            items = events()
            items[0][field] = ["secret"]
            text, passed = summarize(items)
            self.assertFalse(passed)
            self.assertNotIn("secret", text)
        for malformed in ("raw secret", "null", "[]"):
            text, passed = reporter.summarize([*(json.dumps(x) for x in events()), malformed], 0)
            self.assertFalse(passed)
            self.assertNotIn("secret", text)

    def test_typed_diagnostics_never_override_action_outcomes(self):
        for status, kind in ((503, "http"), (429, "http"), (0, "invalid_response"), (0, "transport"), (0, "unknown")):
            payload = dict(source="pubmed", kind=kind, status=status, retryable=True, retryAfterSeconds=30)
            event = self.diagnostic_event(payload)
            text, passed = summarize(events() + [event])
            self.assertTrue(passed)  # only Action terminals decide success
            self.assertIn(f"| pubmed | {kind} | {status} | true | 30 |", text)
            text, passed = summarize([event])
            self.assertFalse(passed)
            self.assertIn("| pubmed | not-run |", text)

    @staticmethod
    def diagnostic_event(payload):
        return dict(Package=reporter.PACKAGE, Test=reporter.TEST + "/pubmed", Action="output",
                    Output="    source_live_test.go:50: GLEAN_SOURCE_DIAGNOSTIC " + json.dumps(payload) + "\n")

    def test_unsafe_malformed_and_duplicate_diagnostics_are_dropped(self):
        base = dict(source="pubmed", kind="http", status=503, retryable=True, retryAfterSeconds=30)
        for field, value in (("secret", "private"), ("source", "private"), ("kind", "private"),
                             ("kind", []), ("status", 999), ("status", True), ("retryable", "private"),
                             ("retryAfterSeconds", -1), ("retryAfterSeconds", 10**500)):
            item = dict(base)
            item[field] = value
            text, passed = summarize(events() + [self.diagnostic_event(item)])
            self.assertTrue(passed)
            self.assertIn("No validated typed diagnostic", text)
            self.assertNotIn("private", text)
        event = self.diagnostic_event(base)
        for bad in (event["Output"] + "private", event["Output"].replace("source_live_test.go", "other.go"),
                    event["Output"].replace('"status": 503', '"status": 503, "status": 429'),
                    "GLEAN_SOURCE_DIAGNOSTIC private"):
            text, passed = summarize(events() + [dict(event, Output=bad)])
            self.assertTrue(passed)
            self.assertIn("No validated typed diagnostic", text)
        text, passed = summarize(events() + [event, event, event])
        self.assertTrue(passed)
        self.assertIn("No validated typed diagnostic", text)
        text, _ = summarize(events() + [dict(event, Test=reporter.TEST + "/arxiv")])
        self.assertIn("No validated typed diagnostic", text)

    def test_actual_workflow_shell_preserves_exit_and_keeps_raw_output_private(self):
        workflow = SCRIPT.parents[2] / ".github/workflows/live-source-acceptance.yml"
        section = workflow.read_text().split("      - name: Probe six official sources without persistence\n", 1)[1]
        shell = textwrap.dedent(section.split("        run: |\n", 1)[1].split("\n      - name:", 1)[0])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake_go = root / "go"
            fake_go.write_text("#!/bin/sh\nprintf 'raw-private-output\\n'\nprintf 'raw-private-error\\n' >&2\nexit \"$FAKE_STATUS\"\n")
            fake_go.chmod(0o700)
            for code in (0, 1, 124):
                env = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                           RUNNER_TEMP=str(root), FAKE_STATUS=str(code))
                result = subprocess.run(["bash", "-e", "-c", shell], env=env, capture_output=True)
                self.assertEqual(result.returncode, code)
                self.assertEqual(result.stdout, b"")
                self.assertEqual(result.stderr, b"")
                runtime = root / "glean-canary"
                self.assertEqual((runtime / "exit-code").read_text().strip(), str(code))
                self.assertIn("raw-private-output", (runtime / "events.jsonl").read_text())
                self.assertIn("raw-private-error", (runtime / "stderr.log").read_text())

    def test_cli_writes_safe_artifact_and_summary_even_when_missing_or_failed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            event, code, artifact, summary = [root / name for name in ("events", "code", "safe/report.md", "summary")]
            for items, status, expected in ((None, None, 1), (events(), "1", 1), (events(), "0", 0)):
                if items is not None:
                    event.write_text("\n".join(json.dumps(x) for x in items))
                    code.write_text(status)
                result = subprocess.run([sys.executable, str(SCRIPT), str(event), str(code), str(artifact), str(summary)], capture_output=True)
                self.assertEqual(result.returncode, expected)
                self.assertEqual(result.stdout, b"")
                self.assertEqual(result.stderr, b"")
                self.assertIn(artifact.read_text(), summary.read_text())


if __name__ == "__main__":
    unittest.main()
