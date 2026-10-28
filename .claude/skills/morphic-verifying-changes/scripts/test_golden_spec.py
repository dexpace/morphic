"""Tests for golden_spec.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-verifying-changes/scripts
"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import golden_spec

HERE = Path(__file__).resolve().parent


class CacheTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        self.cache = Path(self.dir.name)
        self.fetches = []

    def tearDown(self):
        self.dir.cleanup()

    def get(self, body):
        def fetch(sha):
            self.fetches.append(sha)
            return body
        return fetch

    def test_a_spec_is_fetched_once_per_commit(self):
        first = golden_spec.cached("abc", self.cache, self.get(b'{"openapi": "3.0.3"}'))
        again = golden_spec.cached("abc", self.cache, self.get(b"never read"))
        self.assertEqual((first, again, self.fetches), (again, first, ["abc"]))
        self.assertEqual(json.loads(first.read_bytes()), {"openapi": "3.0.3"})

    def test_what_is_not_json_is_never_cached(self):
        with self.assertRaises(ValueError):
            golden_spec.cached("abc", self.cache, self.get(b"<html>rate limited</html>"))
        self.assertFalse((self.cache / "abc" / golden_spec.FILE).exists())


class CountTest(unittest.TestCase):
    def test_objects_carrying_each_key_are_counted_at_any_depth(self):
        doc = {"a": {"allOf": [], "x": [{"allOf": [{"discriminator": {}}]}]}, "allOf": 1}
        self.assertEqual(golden_spec.count_keys(doc, ["allOf", "discriminator", "none"]),
                         {"allOf": 3, "discriminator": 1, "none": 0})

    def test_a_deep_document_does_not_exhaust_the_stack(self):
        doc = {}
        for _ in range(100_000):
            doc = {"n": doc}
        self.assertEqual(golden_spec.count_keys(doc, ["n"]), {"n": 100_000})


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_result(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        script = str(HERE / "golden_spec.py")
        run = subprocess.run([sys.executable, script], capture_output=True, text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
