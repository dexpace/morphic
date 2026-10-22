"""Paths shared by the build scripts.

The catalog's committed inputs live in data/. The corpus they were extracted from is large
and refetchable, so it lives outside the repository, in WORK."""
import os
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
AGENT_DIR = HERE.parent
REPO = Path(subprocess.run(["git", "-C", str(HERE), "rev-parse", "--show-toplevel"],
                           capture_output=True, text=True, check=True).stdout.strip())
WORK = Path(os.environ.get("MORPHIC_REVIEW_WORK")
            or Path(os.environ.get("XDG_CACHE_HOME") or Path.home() / ".cache") / "morphic-review-build")
