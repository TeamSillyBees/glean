"""Real Git-diff fixtures plus an exhaustive fail-closed aggregate contract."""
import copy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import ci_gate
import ci_scope
import test_catalog_diagnostics as fixtures


class DocumentationScopeTests(unittest.TestCase):
    setUp = fixtures.SelectionTests.setUp
    git = fixtures.SelectionTests.git
    save = fixtures.SelectionTests.save

    def scope(self, **extra):
        head = self.git("rev-parse", "HEAD")
        event = {"pull_request": {"base": {"sha": self.base}, "head": {"sha": head}}, **extra}
        return ci_scope.documentation_only(self.repo, "pull_request", event, head)

    def test_only_markdown_documentation_is_allowlisted(self):
        for path in ("README.md", "docs/guide.md", "docs/glean/overview.md"):
            self.assertTrue(ci_scope.documentation_path(path))
        for path in ("AGENTS.md", "web/src/App.tsx", "docs/glean/query-performance.md", "docs/tests/fixture.md",
                     "docs/test.spec.md", "data/baselines/README.md", ".github/workflows/ci.yml", "docs/image.png", "unknown.md"):
            self.assertFalse(ci_scope.documentation_path(path), path)

    def test_docs_pr_skips_but_every_other_event_runs_full(self):
        head = self.save("docs/guide.md")
        self.assertTrue(self.scope())
        for event_name in ("push", "workflow_dispatch", "schedule", None):
            self.assertFalse(ci_scope.documentation_only(self.repo, event_name, {}, head))

    def test_cumulative_diff_and_rename_from_code_cannot_skip(self):
        self.save("internal/feature.go")
        self.save("docs/guide.md")
        self.assertFalse(self.scope())
        self.git("mv", "internal/feature.go", "docs/feature.md")
        self.git("commit", "-qm", "rename")
        # The source file did not exist at the merge-base, so the final diff is
        # genuinely docs-only. A previously committed source must not disappear.
        self.assertTrue(self.scope())
        self.base = self.save("internal/existing.go")
        self.git("mv", "internal/existing.go", "docs/existing.md")
        self.git("commit", "-qm", "rename existing")
        self.assertFalse(self.scope())

    def test_invalid_empty_or_shallow_diff_requires_full_validation(self):
        head = self.git("rev-parse", "HEAD")
        self.assertFalse(self.scope())
        self.assertFalse(ci_scope.documentation_only(self.repo, "pull_request", {}, head))
        self.save("docs/guide.md")
        (self.repo / ".git/shallow").write_text(self.base + "\n")
        self.assertFalse(self.scope())


class AggregateGateTests(unittest.TestCase):
    def needs(self, docs=False):
        return {"changes": {"result": "success", "outputs": {"docs_only": "true" if docs else "false"}},
                **{job: {"result": "skipped" if docs else "success"} for job in ci_gate.JOBS}}

    def test_full_success_is_explicit(self):
        self.assertIn("Complete CI passed", ci_gate.validate(self.needs(), "push"))

    def test_each_incomplete_full_job_blocks(self):
        for job in ci_gate.JOBS:
            for result in ("failure", "cancelled", "skipped", "", None):
                needs = self.needs()
                needs[job]["result"] = result
                with self.subTest(job=job, result=result), self.assertRaises(ValueError):
                    ci_gate.validate(needs, "push")
            needs = self.needs()
            del needs[job]
            with self.assertRaises(ValueError):
                ci_gate.validate(needs, "pull_request")

    def test_docs_reports_skips_honestly_and_never_passes_main(self):
        self.assertIn("NOT run", ci_gate.validate(self.needs(True), "pull_request"))
        for event in ("push", "workflow_dispatch", None):
            with self.assertRaises(ValueError):
                ci_gate.validate(self.needs(True), event)
        for job in ci_gate.JOBS:
            needs = self.needs(True)
            needs[job]["result"] = "failure"
            with self.assertRaises(ValueError):
                ci_gate.validate(needs, "pull_request")

    def test_unknown_or_failed_classifier_never_turns_green(self):
        for changes in ({}, {"result": "failure"}, {"result": "cancelled"}, {"result": "success"},
                        {"result": "success", "outputs": {"docs_only": "maybe"}}):
            needs = self.needs()
            needs["changes"] = copy.deepcopy(changes)
            with self.assertRaises(ValueError):
                ci_gate.validate(needs, "pull_request")
