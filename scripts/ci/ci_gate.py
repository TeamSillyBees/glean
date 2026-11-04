#!/usr/bin/env python3
"""Required aggregate check: never turn a failed/unknown/skipped full suite green."""
import json
import os


JOBS = ("verify", "recovery", "frontend", "containers", "browser")


def validate(needs, event_name):
    changes = needs.get("changes", {})
    if changes.get("result") != "success":
        raise ValueError("Change classification did not succeed")
    mode = changes.get("outputs", {}).get("docs_only")
    if mode not in {"true", "false"}:
        raise ValueError("Missing or invalid validation scope")
    if mode == "true":
        if event_name != "pull_request":
            raise ValueError("Only PRs may use documentation-only validation")
        expected = "skipped"
        message = "Documentation-only PR validated. Runtime, database, browser and container tests were intentionally NOT run. Main push still requires complete validation."
    else:
        expected = "success"
        message = "Complete CI passed: Go/PostgreSQL/history, recovery, frontend, containers and real browser acceptance."
    invalid = [name for name in JOBS if needs.get(name, {}).get("result") != expected]
    if invalid:
        raise ValueError("Required validation did not finish as expected: " + ", ".join(invalid))
    return message


def main():
    message = validate(json.loads(os.environ["CI_NEEDS_JSON"]), os.environ.get("GITHUB_EVENT_NAME"))
    print(message)
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as summary:
        summary.write(message + "\n")


if __name__ == "__main__":
    main()
