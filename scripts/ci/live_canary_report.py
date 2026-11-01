"""Publish allowlisted outcomes and typed diagnostics, never raw test Output."""
import json
import math
import re
from pathlib import Path
import sys

SOURCES = ("crossref", "pubmed", "arxiv", "biorxiv", "europepmc", "medrxiv")
PACKAGE = "github.com/teamsillybees/glean/internal/modules/monitor"
TEST = "TestLiveSourceCanary"
KINDS = {"unknown", "http", "transport", "invalid_response", "provider_backoff",
         "provider_forbidden", "disabled", "missing_registry"}
DIAGNOSTIC = re.compile(r"^\s+source_live_test\.go:\d+: GLEAN_SOURCE_DIAGNOSTIC (\{[^\n]*\})\n?$")


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate field")
        result[key] = value
    return result


def diagnostic(event):
    """Read a single controlled log event; metadata never changes test outcomes."""
    output = event.get("Output")
    if not isinstance(output, str):
        return None
    match = DIAGNOSTIC.fullmatch(output)
    if not match:
        return None
    try:
        item = json.loads(match.group(1), object_pairs_hook=unique_object)
        if not isinstance(item, dict) or set(item) != {"source", "kind", "status", "retryable", "retryAfterSeconds"}:
            return None
        if not isinstance(item["source"], str) or item["source"] not in SOURCES:
            return None
        if event.get("Test") != TEST + "/" + item["source"]:
            return None
        if not isinstance(item["kind"], str) or item["kind"] not in KINDS:
            return None
        if type(item["status"]) is not int or not (item["status"] == 0 or 100 <= item["status"] <= 599):
            return None
        if type(item["retryable"]) is not bool or type(item["retryAfterSeconds"]) is not int or not 0 <= item["retryAfterSeconds"] <= 9223372036:
            return None
        return item
    except (ValueError, TypeError):
        return None


def summarize(lines, exit_code):
    outcomes = {}
    diagnostics = {}
    seen_diagnostics = set()
    invalid = False
    for line in lines:
        try:
            event = json.loads(line)
            if not isinstance(event, dict):
                invalid = True
                continue
        except (ValueError, TypeError):
            invalid = True
            continue
        if not isinstance(event.get("Action"), str):
            invalid = True
            continue
        if event.get("Package") == PACKAGE and event.get("Action") == "output":
            item = diagnostic(event)
            if item is not None:
                source = item["source"]
                if source in seen_diagnostics:
                    diagnostics.pop(source, None)
                else:
                    diagnostics[source] = item
                    seen_diagnostics.add(source)
        if event.get("Package") != PACKAGE or event.get("Action") not in {"pass", "fail", "skip"}:
            continue
        name = event.get("Test", "")
        if not isinstance(name, str):
            invalid = True
            continue
        if name not in ("", TEST, *(TEST + "/" + source for source in SOURCES)):
            continue
        elapsed = event.get("Elapsed")
        if (isinstance(elapsed, bool) or not isinstance(elapsed, (int, float))
                or not 0 <= elapsed <= 3600 or not math.isfinite(elapsed) or name in outcomes):
            invalid = True
            continue
        outcomes[name] = (event["Action"], elapsed)
    passed = (exit_code == 0 and not invalid and all(
        outcomes.get(name, (None,))[0] == "pass"
        for name in ("", TEST, *(TEST + "/" + source for source in SOURCES))))
    rows = ["# Six-source compatibility probe", "",
            "Result: " + ("PASS" if passed else "FAIL / incomplete"), "",
            "| Source | Outcome | Duration (s) |", "|---|---|---:|"]
    for source in SOURCES:
        outcome, elapsed = outcomes.get(TEST + "/" + source, ("not-run", None))
        rows.append(f"| {source} | {outcome} | {elapsed:.3f} |" if elapsed is not None
                    else f"| {source} | {outcome} | — |")
    rows.extend(["", "## Typed diagnostics (not pass/fail authority)", "",
                 "| Source | Kind | HTTP status | Retryable | Retry-After (s) |", "|---|---|---:|---|---:|"])
    for source in SOURCES:
        item = diagnostics.get(source)
        if item is not None:
            rows.append(f"| {source} | {item['kind']} | {item['status']} | {str(item['retryable']).lower()} | {item['retryAfterSeconds']} |")
    if not diagnostics:
        rows.append("No validated typed diagnostic was emitted; any failure cause remains unknown.")
    rows.extend(["", "Fixed one-day window; at most 7 Fetch calls, no retries, LLM or persistence.",
                 "Not exhaustive coverage or the separate EuropePMC persistence/browser acceptance.",
                 "Failure may reflect upstream availability; it is not automatically a code regression.", ""])
    return "\n".join(rows), passed


def main():
    events, status, destination, summary = map(Path, sys.argv[1:])
    try:
        exit_code = int(status.read_text().strip())
        with events.open(encoding="utf-8") as stream:
            report, passed = summarize(stream, exit_code)
    except (OSError, ValueError, UnicodeError):
        report, passed = summarize([], None)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(report, encoding="utf-8")
    with summary.open("a", encoding="utf-8") as stream:
        stream.write(report)
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
