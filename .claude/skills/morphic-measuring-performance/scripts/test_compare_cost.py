"""Tests for compare_cost.

Run: python3 -m unittest discover -s .claude/skills/morphic-measuring-performance/scripts
"""
import platform
import subprocess
import sys
import unittest
from pathlib import Path
from unittest import mock

import compare_cost as cc

HERE = Path(__file__).resolve().parent


class InstructionsTest(unittest.TestCase):
    def test_each_systems_report_is_read(self):
        darwin = ("        1.23 real\n          10444920  instructions retired\n"
                  "   99  cycles elapsed\n")
        linux = "noise\n987654,,instructions:u,123,100.00,,\n"
        self.assertEqual(cc.instructions(darwin, "Darwin"), 10444920)
        self.assertEqual(cc.instructions(linux, "Linux"), 987654)
        self.assertIsNone(cc.instructions(darwin, "Linux"))
        self.assertIsNone(cc.instructions("", "Windows"))


class WrapperTest(unittest.TestCase):
    def test_each_system_gets_the_counter_its_report_is_read_from(self):
        with mock.patch.object(cc.shutil, "which", return_value="/usr/bin/perf"):
            linux = cc.wrapper("Linux")
        self.assertEqual(linux[:2], ["perf", "stat"])
        self.assertIn("-x,", linux, "instructions() reads perf's comma-separated report")
        self.assertIn("instructions:u", linux)
        with mock.patch.object(cc.Path, "is_file", return_value=True):
            self.assertEqual(cc.wrapper("Darwin"), ["/usr/bin/time", "-l"])
        with mock.patch.object(cc.shutil, "which", return_value=None):
            self.assertEqual(cc.wrapper("Linux"), [], "no perf, no instruction count")
        with mock.patch.object(cc.Path, "is_file", return_value=False):
            self.assertEqual(cc.wrapper("Darwin"), [])
        self.assertEqual(cc.wrapper("Windows"), [])


class MeasureTest(unittest.TestCase):
    def test_a_run_reports_its_status_and_cost(self):
        grow = "import sys; b = bytearray(64 << 20); sys.exit(3)"
        got = cc.measure([sys.executable, "-c", grow], [], platform.system())
        self.assertEqual(got["status"], 3)
        self.assertGreater(got["rss"], 64 << 20, "peak memory counts the child's 64 MiB")
        self.assertGreater(got["wall"], 0)
        self.assertIsNone(got["instructions"], "no wrapper, no instruction count")

    def test_peak_memory_is_in_bytes_on_every_system(self):
        self.assertEqual(cc.peak_bytes(5, "Darwin"), 5)
        self.assertEqual(cc.peak_bytes(5, "Linux"), 5 * 1024)


class TableTest(unittest.TestCase):
    def run_of(self, instructions, status=0):
        return {"status": status, "instructions": instructions, "cpu": 1.0, "rss": 2**20,
                "wall": 2.0}

    def test_medians_ranges_and_ratio(self):
        base = [self.run_of(i) for i in (100e6, 110e6, 90e6)]
        head = [self.run_of(i) for i in (220e6, 200e6, 180e6)]
        lines = cc.table(base, head)
        row = next(l for l in lines if l.startswith("instructions"))
        self.assertIn("100.000M [90.000M..110.000M]", row)
        self.assertTrue(row.endswith("2.000"), row)

    def test_a_missing_measure_is_left_out_and_differing_exits_are_named(self):
        lines = cc.table([self.run_of(None, 0)], [self.run_of(None, 1)])
        self.assertFalse(any(l.startswith("instructions") for l in lines))
        self.assertIn("base and head exit differently, so they did different work", lines)
        lines = cc.table([self.run_of(1, 0), self.run_of(1, 1)], [self.run_of(1, 0)])
        self.assertIn("base runs exited differently: [0, 1]", lines)


class CrashTest(unittest.TestCase):
    def test_a_missing_tool_exits_2_never_a_verdict(self):
        env = {"PATH": "", "PYTHONDONTWRITEBYTECODE": "1"}
        run = subprocess.run([sys.executable, str(HERE / "compare_cost.py"), "--", "x"],
                             capture_output=True, text=True, env=env)
        self.assertEqual(run.returncode, 2, run.stderr)
        self.assertEqual(run.stdout, "")


if __name__ == "__main__":
    unittest.main()
