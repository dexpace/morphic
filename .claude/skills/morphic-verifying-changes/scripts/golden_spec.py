#!/usr/bin/env python3.14
# /// script
# requires-python = ">=3.14"
# ///
"""Fetch the golden spec (CLAUDE.md, "Priority") once at a pinned revision, and print its path
or count what it holds.

Usage: python3.14 golden_spec.py [--rev REV] [--count KEY...]

REV is a commit of cloudflare/api-schemas (default its current HEAD, resolved to a commit so the
result names what was measured). The spec is cached outside the repository, under the system's
temporary directory, one file per commit. Without --count, prints the cached file's path, for
use as $(python3.14 golden_spec.py). With --count, prints how many JSON objects in the spec carry
each KEY. The commit measured goes to stderr. Exit 0 on success, 2 when it cannot fetch or read.
"""

import argparse
import json
import subprocess
import sys
import tempfile
import traceback
from collections.abc import Callable
from pathlib import Path

REPO = "cloudflare/api-schemas"
FILE = "openapi.json"


def gh(*args: str) -> bytes:
    return subprocess.run(["gh", "api", *args], check=True, capture_output=True).stdout


def resolve(rev: str) -> str:
    return gh(f"repos/{REPO}/commits/{rev}", "--jq", ".sha").decode().strip()


def fetch(sha: str) -> bytes:
    return gh("-H", "Accept: application/vnd.github.raw", f"repos/{REPO}/contents/{FILE}?ref={sha}")


def cached(sha: str, cache: Path, get: Callable[[str], bytes] = fetch) -> Path:
    """Return the spec's path at commit sha under cache, fetching it only when absent. A fetch
    lands in a temporary file first, so an interrupted one never leaves a partial spec, and is
    kept only when it is an OpenAPI document: the contents API answers with JSON about the file,
    not the file, when the raw form is not served."""
    path = cache / sha / FILE
    if path.is_file():
        return path
    path.parent.mkdir(parents=True, exist_ok=True)
    partial = path.with_suffix(".partial")
    partial.write_bytes(get(sha))
    try:
        doc = json.loads(partial.read_bytes())  # raises for what is not JSON
        if not isinstance(doc, dict) or "openapi" not in doc:
            raise ValueError(f"{REPO}@{sha}:{FILE} fetched as no OpenAPI document")
    except ValueError:
        partial.unlink()
        raise
    partial.replace(path)
    return path


def count_keys(document: object, keys: list[str]) -> dict[str, int]:
    """Count the JSON objects anywhere in document that carry each key, without recursion, so a
    deep document cannot exhaust the stack."""
    counts = dict.fromkeys(keys, 0)
    stack = [document]
    while stack:
        node = stack.pop()
        if isinstance(node, dict):
            for k in keys:
                if k in node:
                    counts[k] += 1
            stack.extend(node.values())
        elif isinstance(node, list):
            stack.extend(node)
    return counts


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Fetch and measure the golden spec.")
    p.add_argument("--rev", default="HEAD")
    p.add_argument("--count", nargs="+", metavar="KEY")
    args = p.parse_args(sys.argv[1:] if argv is None else argv)
    try:
        sha = resolve(args.rev)
        path = cached(sha, Path(tempfile.gettempdir()) / "morphic-golden-spec")
    except subprocess.CalledProcessError as e:
        print(f"golden_spec: {(e.stderr or b'').decode().strip() or e}", file=sys.stderr)
        return 2
    except ValueError as e:  # a fetch that is not the spec, kept out of the cache
        print(f"golden_spec: {e}", file=sys.stderr)
        return 2
    print(f"golden spec {REPO}@{sha}: {path}", file=sys.stderr)
    if not args.count:
        print(path)
        return 0
    for key, n in count_keys(json.loads(path.read_bytes()), args.count).items():
        print(f"{key}: {n}")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception:  # a crash is no verdict: exit 2, never a code that means one
        traceback.print_exc()
        sys.exit(2)
