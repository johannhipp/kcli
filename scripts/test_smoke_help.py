"""Regression tests for dependency-free smoke-script help."""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path


SCRIPTS = Path(__file__).resolve().parent


class SmokeHelpTests(unittest.TestCase):
    def test_help_does_not_require_optional_api_package(self) -> None:
        for name in ("smoke_anonymous.py", "smoke_authenticated_readonly.py"):
            with self.subTest(script=name):
                result = subprocess.run(
                    [sys.executable, str(SCRIPTS / name), "--help"],
                    capture_output=True,
                    check=False,
                    text=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("usage:", result.stdout)


if __name__ == "__main__":
    unittest.main()
