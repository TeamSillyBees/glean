#!/usr/bin/env python3
"""Only proven, allowlisted documentation-only PRs may omit runtime validation."""
import json
import os
from pathlib import Path, PurePosixPath

from catalog_diagnostics import safe_path, select


def documentation_path(path):
    # Do not classify tests, data, generated material, workflows, images or unknown
    # root Markdown as documentation. Performance contracts always get full CI.
    return (path == "README.md" or path.startswith("docs/") and PurePosixPath(path).suffix == ".md") and safe_path(path)


def documentation_only(repo, event_name, event, checkout_sha):
    if event_name != "pull_request":
        return False
    enabled, _ = select(repo, event_name, event, checkout_sha, allow_path=documentation_path)
    return not enabled


def main():
    try:
        event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
        docs = documentation_only(Path.cwd(), os.environ.get("GITHUB_EVENT_NAME"), event, os.environ.get("GITHUB_SHA"))
    except (OSError, ValueError, KeyError):
        docs = False
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"docs_only={'true' if docs else 'false'}\n")
    print("Documentation-only PR: runtime tests will be explicitly skipped." if docs else "Complete validation required.")


if __name__ == "__main__":
    main()
