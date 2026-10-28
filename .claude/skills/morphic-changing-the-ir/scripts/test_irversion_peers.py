"""Tests for irversion_peers.

Run: python3.14 -m unittest discover -s .claude/skills/morphic-changing-the-ir/scripts
"""
import io
import os
import subprocess
import sys
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest import mock

import irversion_peers as ip

HERE = Path(__file__).resolve().parent


class DeclaredTest(unittest.TestCase):
    def test_the_constant_is_read_and_nothing_else(self):
        text = 'package ir\n\n// IRVersion = "9.9.9" in a comment\nconst IRVersion = "0.21.0"\n'
        self.assertEqual(ip.declared(text), "0.21.0")
        self.assertIsNone(ip.declared('package ir\nvar IRVersion = "0.1.0"\n'))

    def test_versions_order_by_number_not_text(self):
        self.assertGreater(ip.order("0.10.0"), ip.order("0.9.0"))
        self.assertIsNone(ip.order("0.21"))
        self.assertIsNone(ip.order("0.21.0-rc1"))


class JudgeTest(unittest.TestCase):
    def test_an_unmoved_tree_collides_with_nothing(self):
        lines, bad = ip.judge("0.21.0", "0.21.0", [(7, "feat!: x", "0.22.0", "0.21.0")])
        self.assertFalse(bad)
        self.assertIn("#7         0.22.0  feat!: x", lines)

    def test_a_shared_claim_collides(self):
        peers = [(7, "a", "0.22.0", "0.21.0"), (8, "b", "0.21.0", "0.21.0"),
                 (9, "c", "0.13.0", "0.13.0"), (10, "d", "0.21.0", None)]
        lines, bad = ip.judge("0.21.0", "0.22.0", peers)
        self.assertTrue(bad)
        self.assertTrue(any(l.startswith("COLLIDES   #7") for l in lines), lines)
        self.assertFalse(any("#8" in l or "#9" in l or "#10" in l for l in lines),
                         "a PR that did not move its base's version claims nothing")

    def test_a_bump_from_a_base_main_has_passed_is_listed(self):
        lines, bad = ip.judge("0.21.0", "0.22.0", [(7, "a", "0.21.0", "0.20.0")])
        self.assertFalse(bad, "its claim past main is not known until it merges main")
        self.assertTrue(any(l.startswith("#7") and "bumped from 0.20.0" in l for l in lines), lines)

    def test_a_claim_main_has_reached_or_passed_trails(self):
        for tree in ("0.20.0", "0.9.0", "next"):
            with self.subTest(tree=tree):
                lines, bad = ip.judge("0.21.0", tree, [])
                self.assertTrue(bad)
                self.assertTrue(any(l.startswith("TRAILS") for l in lines), lines)

    def test_a_claim_ahead_of_main_with_no_peer_is_clean(self):
        self.assertFalse(ip.judge("0.9.0", "0.10.0", [(7, "a", "0.11.0", "0.9.0")])[1])


class RemoteTest(unittest.TestCase):
    def test_gh_is_pointed_at_the_remote_before_any_call(self):
        with mock.patch.dict(os.environ, {"REMOTE": "upstream"}), \
                mock.patch.object(ip, "point_gh_at", side_effect=RuntimeError("stop")) as point, \
                mock.patch.object(ip, "peers_of") as peers, redirect_stderr(io.StringIO()):
            self.assertEqual(ip.main(), 2)
        self.assertEqual(point.call_args.args[1], "upstream")
        peers.assert_not_called()


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "irversion_peers.py")],
                             capture_output=True, text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
