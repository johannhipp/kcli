"""Offline checks for release version selection and changelog preservation."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from prepare_release import next_version, update_changelog


class ReleaseTests(unittest.TestCase):
    def test_versions(self):
        self.assertEqual(next_version(None, "feat!: initial release"), "0.1.0")
        self.assertEqual(next_version("v0.1.0", "fix(search): correct filter"), "0.1.1")
        self.assertEqual(next_version("v0.1.1", "feat(search): add sort"), "0.2.0")
        self.assertEqual(next_version("v0.2.0", "feat(cli)!: change schema"), "1.0.0")
        self.assertEqual(next_version("v1.2.3", "refactor: remove old contract\n\nBREAKING CHANGE: output changes"), "2.0.0")
        self.assertEqual(next_version("v1.2.3", "docs: clarify flags"), "1.2.4")

    def test_changelog_keeps_history_and_moves_pending(self):
        before = "# Changelog\n\n## [Unreleased]\n\n### Fixed\n\n- Fix filters.\n\n## [0.1.0] - 2026-09-01\n\n### Added\n\n- Initial release.\n\n[Unreleased]: https://old\n[0.1.0]: https://old\n"
        after, notes = update_changelog(before, "0.1.1", "2026-09-18", "unused", "https://github.com/johannhipp/kcli")
        self.assertIn("## [Unreleased]\n\n## [0.1.1] - 2026-09-18", after)
        self.assertIn("- Initial release.", after)
        self.assertEqual(notes, "### Fixed\n\n- Fix filters.\n")
        self.assertIn("compare/v0.1.0...v0.1.1", after)
        self.assertEqual(after.count("[Unreleased]:"), 1)
        self.assertNotIn("https://old", after)

    def test_prepare_and_retry_use_one_immutable_tag(self):
        script = Path(__file__).with_name("prepare_release.py").resolve()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            def git(*args):
                return subprocess.check_output(["git", *args], cwd=root, text=True, stderr=subprocess.DEVNULL).strip()
            git("init", "-q")
            git("config", "user.name", "Release test")
            git("config", "user.email", "test@example.invalid")
            (root / "CHANGELOG.md").write_text("# Changelog\n\n## [Unreleased]\n\n### Added\n\n- Initial release.\n")
            git("add", ".")
            git("commit", "-qm", "feat: initial release")
            source = git("rev-parse", "HEAD")
            env = {**os.environ, "GITHUB_OUTPUT": str(root / "output"), "RUNNER_TEMP": directory, "GITHUB_REPOSITORY": "johannhipp/kcli"}
            subprocess.run(["python3", str(script)], cwd=root, env=env, check=True, capture_output=True)
            release = git("rev-parse", "HEAD")
            self.assertEqual(git("tag"), "v0.1.0")
            self.assertIn("publish_commit=true", (root / "output").read_text())
            git("checkout", "-q", "--detach", source)
            (root / "output").write_text("")
            subprocess.run(["python3", str(script)], cwd=root, env=env, check=True, capture_output=True)
            self.assertEqual(git("rev-parse", "HEAD"), release)
            self.assertEqual(git("tag"), "v0.1.0")
            self.assertIn("publish_commit=false", (root / "output").read_text())

    def test_empty_pending_uses_commit_notes(self):
        after, notes = update_changelog("# Changelog\n\n## [Unreleased]\n", "0.1.0", "2026-09-18", "### Changed\n\n- docs: improve guide", "https://github.com/johannhipp/kcli")
        self.assertIn("docs: improve guide", notes)
        self.assertIn("releases/tag/v0.1.0", after)


if __name__ == "__main__":
    unittest.main()
