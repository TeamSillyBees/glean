"""Offline event fixtures backed by real temporary Git histories."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "catalog_diagnostics.py"
spec = importlib.util.spec_from_file_location("catalog_diagnostics", SCRIPT)
selector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selector)


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name)
        self.git("init", "-q")
        self.git("config", "user.email", "test@example.invalid")
        self.git("config", "user.name", "Offline Test")
        self.base = self.save("README.md", "initial")

    def git(self, *args):
        return selector.git(self.repo, *args).decode().strip()

    def save(self, path, content="fixture"):
        file = self.repo / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(content)
        self.git("add", "--", path)
        self.git("commit", "-qm", "fixture")
        return self.git("rev-parse", "HEAD")

    def push(self, base=None, **event):
        head = self.git("rev-parse", "HEAD")
        return selector.select(self.repo, "push", {"before": base or self.base, "after": head, **event}, head)[0]

    def test_allowlist(self):
        for path in ("web/src/App.tsx", "web/public/icon.svg", "docs/guide.md", "README.md", "web/DESIGN.md"):
            with self.subTest(path=path):
                self.assertTrue(selector.safe_path(path))
        for path in ("internal/main.go", "data/x.md", "db/schema.sql", "go.mod", "go.sum",
                     "web/package.json", "web/package-lock.json", "web/vite.config.ts", "web/e2e/x.ts",
                     "web/src/App.test.tsx", "web/src/__tests__/x.ts", "tests/readme.md",
                     "docs/query-performance.md", "docs/glean/query-performance.md", ".github/workflows/ci.yml",
                     "scripts/ci/catalog_diagnostics.py", "unknown.md", "docs/../internal/x.md", ""):
            with self.subTest(path=path):
                self.assertFalse(selector.safe_path(path))

    def test_main_always_runs_even_for_docs_or_invalid_payload(self):
        self.save("docs/guide.md")
        self.assertTrue(self.push(ref="refs/heads/main"))
        self.assertTrue(selector.select(self.repo, "push", {"ref": "refs/heads/main"}, None)[0])

    def test_frontend_and_docs_skip(self):
        self.save("web/src/App.tsx")
        self.save("docs/guide.md")
        self.assertFalse(self.push())

    def test_all_pushed_commits_not_just_parent(self):
        self.save("internal/backend.go")
        self.save("web/src/App.tsx")
        self.assertTrue(self.push())

    def test_rename_checks_old_path(self):
        base = self.save("internal/backend.ts")
        (self.repo / "web/src").mkdir(parents=True)
        self.git("mv", "internal/backend.ts", "web/src/backend.ts")
        self.git("commit", "-qm", "rename")
        self.assertTrue(self.push(base))

    def test_deleted_backend_runs(self):
        base = self.save("internal/backend.go")
        self.git("rm", "internal/backend.go")
        self.git("commit", "-qm", "delete")
        self.assertTrue(self.push(base))

    def test_unusual_filename_is_not_split(self):
        self.save("web/src/new\nname.tsx")
        self.assertFalse(self.push())
        self.save("internal/new\nweb/src/evil.tsx")
        self.assertTrue(self.push())

    def test_ambiguous_events_and_history_run(self):
        head = self.save("web/src/App.tsx")
        for name, event, sha in [
            ("workflow_dispatch", {}, head), ("schedule", {}, head),
            ("push", {}, head), ("push", {"before": "0" * 40, "after": head}, head),
            ("push", {"before": "f" * 40, "after": head}, head),
            ("push", {"before": "HEAD~1", "after": head}, head),
            ("push", {"before": self.base, "after": head}, self.base),
            ("push", {"before": self.base, "after": self.base}, head),
            ("push", {"before": head, "after": head}, head),
            ("pull_request", {}, head), ("push", None, head),
        ]:
            with self.subTest(name=name, event=event):
                self.assertTrue(selector.select(self.repo, name, event, sha)[0])
        self.assertTrue(self.push(forced=True))
        self.assertTrue(self.push(deleted=True))
        (self.repo / ".git/shallow").write_text(self.base + "\n")
        self.assertTrue(self.push())

    def test_diff_error_does_not_use_partial_output(self):
        self.save("web/src/App.tsx")
        real_git = selector.git
        def broken(repo, *args):
            if args[0] == "diff":
                raise subprocess.CalledProcessError(1, "git diff", output=b"web/src/App.tsx\0")
            return real_git(repo, *args)
        with patch.object(selector, "git", side_effect=broken):
            self.assertTrue(self.push())

    def test_pr_uses_merge_base_not_base_tip_or_merge_parent(self):
        self.git("checkout", "-qb", "feature")
        head = self.save("web/src/App.tsx")
        self.git("checkout", "-qb", "base", self.base)
        base_tip = self.save("internal/base-only.go")
        self.git("merge", "--no-ff", "-qm", "merge", "feature")
        merge = self.git("rev-parse", "HEAD")
        event = {"pull_request": {"base": {"sha": base_tip}, "head": {"sha": head}}}
        self.assertFalse(selector.select(self.repo, "pull_request", event, merge)[0])
        self.git("checkout", "-q", "feature")
        head = self.save("internal/pr.go")
        event["pull_request"]["head"]["sha"] = head
        self.git("checkout", "-q", "base")
        self.git("merge", "--no-ff", "-qm", "merge", "feature")
        self.assertTrue(selector.select(self.repo, "pull_request", event, self.git("rev-parse", "HEAD"))[0])

    def test_invalid_event_cli_enables_and_unexpected_failure_is_nonzero(self):
        event = self.repo / "event.json"
        output = self.repo / "output"
        event.write_text("not json")
        env = {**os.environ, "GITHUB_EVENT_PATH": str(event), "GITHUB_OUTPUT": str(output)}
        result = subprocess.run(["python3", str(SCRIPT)], cwd=self.repo, env=env, capture_output=True)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(output.read_text(), "enabled=1\n")
        env["GITHUB_OUTPUT"] = str(self.repo / "missing/output")
        result = subprocess.run(["python3", str(SCRIPT)], cwd=self.repo, env=env, capture_output=True)
        self.assertNotEqual(result.returncode, 0)


class RequiredPassTests(unittest.TestCase):
    """Execute the workflow's real assertion against synthetic go test JSON."""
    def setUp(self):
        workflow = (SCRIPT.parents[2] / ".github/workflows/ci.yml").read_text()
        import textwrap
        block = workflow.split('"$(go list -m)" <<\'PY\'\n', 1)[1].split('\n          PY', 1)[0]
        self.code = textwrap.dedent(block)
        # Read the unconditional contract directly, rather than duplicating its list.
        namespace = {"module": "fixture"}
        contract = self.code.split("required =", 1)[1].split("diagnostic =", 1)[0]
        exec("required =" + contract, namespace)
        self.required = namespace["required"]
        self.diagnostic = ("fixture/internal/modules/catalog", "TestCatalogHistoricalQueryDiagnostics")

    def check_result(self, enabled, diagnostic_action, omit=None):
        with tempfile.TemporaryDirectory() as tmp:
            results = Path(tmp) / "results.jsonl"
            events = [{"Package": p, "Test": t, "Action": "pass"} for p, t in self.required if (p, t) != omit]
            if diagnostic_action:
                events.append({"Package": self.diagnostic[0], "Test": self.diagnostic[1], "Action": diagnostic_action})
            results.write_text("\n".join(json.dumps(e) for e in events))
            return subprocess.run(["python3", "-c", self.code, str(results), "fixture"],
                                  env={**os.environ, "GLEAN_CATALOG_QUERY_DIAGNOSTICS": enabled},
                                  capture_output=True).returncode

    def test_enabled_requires_pass_even_for_missing_or_skipped_probe(self):
        for enabled in ("1", "", "unknown"):
            self.assertEqual(self.check_result(enabled, "pass"), 0)
            self.assertNotEqual(self.check_result(enabled, "skip"), 0)
            self.assertNotEqual(self.check_result(enabled, None), 0)

    def test_disabled_requires_explicit_skip_and_every_semantic_test(self):
        self.assertEqual(self.check_result("0", "skip"), 0)
        self.assertNotEqual(self.check_result("0", "pass"), 0)
        self.assertNotEqual(self.check_result("0", None), 0)
        for required in self.required:
            with self.subTest(required=required):
                self.assertNotEqual(self.check_result("0", "skip", omit=required), 0)


if __name__ == "__main__":
    unittest.main()
