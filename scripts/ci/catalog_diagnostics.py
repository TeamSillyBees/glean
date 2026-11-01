#!/usr/bin/env python3
"""Skip only the optional timing probe for a proven frontend/documentation diff.

No API calls or changed-file truncation. Every uncertainty defaults to running.
"""
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess


SHA = re.compile(r"[0-9a-f]{40}\Z")
ROOT_DOCS = {"README.md", "CHANGELOG.md", "CONTRIBUTING.md", "GLOSSARY.md", "SECURITY.md", "TEMPLATE_ORIGIN.md"}


def git(repo, *args):
    return subprocess.run(["git", "-C", str(repo), *args], check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout


def commit(repo, value):
    if not isinstance(value, str) or not SHA.fullmatch(value) or value == "0" * 40:
        raise ValueError("missing or invalid commit")
    if git(repo, "rev-parse", "--verify", value + "^{commit}").decode().strip() != value:
        raise ValueError("not an exact commit")
    return value


def safe_path(path):
    parts = PurePosixPath(path).parts
    if not parts or path.startswith("/") or any(p in {".", ".."} for p in path.split("/")):
        return False
    # Test/dependency/configuration/unknown paths deliberately retain the probe.
    if any(p.lower() in {"test", "tests", "__tests__", "e2e"} for p in parts):
        return False
    if re.search(r"(?:^|[._-])(?:test|spec)(?:[._-]|$)", parts[-1], re.I):
        return False
    if parts[-1] == "query-performance.md":
        return False
    if path in ROOT_DOCS:
        return True
    if path.startswith("docs/") and path.endswith(".md"):
        return True
    if len(parts) == 2 and parts[0] == "web" and parts[1] in {"README.md", "DESIGN.md", "PRODUCT.md", "COMPONENTS.md"}:
        return True
    if path.startswith("web/src/") and PurePosixPath(path).suffix in {".ts", ".tsx", ".css", ".svg"}:
        return True
    return path.startswith("web/public/") and PurePosixPath(path).suffix in {".svg", ".png", ".jpg", ".webp", ".ico"}


def select(repo, event_name, event, checkout_sha):
    """Return (enabled, constant reason); no untrusted path/event text in outputs."""
    if event_name not in {"push", "pull_request"}:
        return True, "manual or unsupported event"
    try:
        current = commit(repo, checkout_sha)
        if git(repo, "rev-parse", "HEAD").decode().strip() != current:
            raise ValueError("checkout mismatch")
        if git(repo, "rev-parse", "--is-shallow-repository").strip() != b"false":
            raise ValueError("incomplete history")
        if event_name == "push":
            if event.get("deleted") or event.get("forced"):
                raise ValueError("ambiguous push")
            base, head = commit(repo, event.get("before")), commit(repo, event.get("after"))
            if head != current:
                raise ValueError("push checkout mismatch")
            git(repo, "merge-base", "--is-ancestor", base, head)
        else:
            pr = event["pull_request"]
            base, head = commit(repo, pr["base"]["sha"]), commit(repo, pr["head"]["sha"])
            git(repo, "merge-base", "--is-ancestor", head, current)
            git(repo, "merge-base", "--is-ancestor", base, current)
            bases = git(repo, "merge-base", "--all", base, head).decode().splitlines()
            if len(bases) != 1:
                raise ValueError("ambiguous merge base")
            base = commit(repo, bases[0])
        # Disable rename detection so both old and new paths must be safe.
        raw = git(repo, "diff", "--name-only", "--no-renames", "-z", base, head, "--")
        if not raw or not raw.endswith(b"\0"):
            return True, "empty or ambiguous diff"
        paths = raw[:-1].decode("utf-8", errors="strict").split("\0")
        if not all(safe_path(path) for path in paths):
            return True, "changes outside frontend/documentation allowlist"
        return False, "proven frontend/documentation-only diff"
    except (OSError, subprocess.SubprocessError, ValueError, KeyError, TypeError, AttributeError):
        return True, "event, history or diff could not be proven safe"


def main():
    try:
        event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
        enabled, reason = select(Path.cwd(), os.environ.get("GITHUB_EVENT_NAME"), event,
                                 os.environ.get("GITHUB_SHA"))
    except (OSError, ValueError, KeyError):
        enabled, reason = True, "event unavailable or invalid"
    # An unexpected script/output failure fails the step, never emits a skip fallback.
    value = "1" if enabled else "0"
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"enabled={value}\n")
    print(f"Historical query diagnostics: {'RUN' if enabled else 'SKIP'} ({reason}).")


if __name__ == "__main__":
    main()
