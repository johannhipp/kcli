#!/usr/bin/env python3
"""Validate repository documentation contracts without network access."""

from __future__ import annotations

import re
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
MARKDOWN_FILES = [
    *sorted(ROOT.glob("*.md")),
    *sorted((ROOT / "docs").glob("*.md")),
]
STORY_FILES = {
    ROOT / "docs" / "user-stories.md": r"US-[A-Z]+-\d+",
    ROOT / "docs" / "v0.1-scope.md": r"V01-[A-Z]+-\d+",
}
COVERAGE_FILE = ROOT / "docs" / "endpoint-coverage.md"
IMPLEMENTATION_FILE = ROOT / "docs" / "implementation-plan.md"


def check_local_links() -> list[str]:
    errors: list[str] = []
    for source in MARKDOWN_FILES:
        text = source.read_text(encoding="utf-8")
        if not text.endswith("\n"):
            errors.append(f"{source.relative_to(ROOT)}: missing final newline")
        for target in re.findall(r"\[[^]]+\]\(([^)]+)\)", text):
            path_part = target.split("#", 1)[0]
            if not path_part or "://" in path_part:
                continue
            if not (source.parent / path_part).resolve().exists():
                errors.append(
                    f"{source.relative_to(ROOT)}: broken local link {target!r}"
                )
    return errors


def check_stories() -> list[str]:
    errors: list[str] = []
    all_ids: set[str] = set()
    for source, id_pattern in STORY_FILES.items():
        for line_number, line in enumerate(source.read_text(encoding="utf-8").splitlines(), 1):
            if not re.match(rf"^- \*\*{id_pattern}\*\*", line):
                continue
            match = re.fullmatch(
                rf"- \*\*({id_pattern})\*\* — As an? .+, I want .+, so that .+\.",
                line,
            )
            if not match:
                errors.append(
                    f"{source.relative_to(ROOT)}:{line_number}: invalid story format"
                )
                continue
            story_id = match.group(1)
            if story_id in all_ids:
                errors.append(f"duplicate story ID: {story_id}")
            all_ids.add(story_id)
    return errors


def check_changelog() -> list[str]:
    errors: list[str] = []
    text = (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    headings = re.findall(r"^## \[([^]]+)\](?: - ([^\n]+))?$", text, re.MULTILINE)
    if not headings or headings[0][0] != "Unreleased":
        errors.append("CHANGELOG.md: first version heading must be [Unreleased]")
    semver = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")
    date = re.compile(r"^\d{4}-\d{2}-\d{2}$")
    for version, release_date in headings[1:]:
        if not semver.fullmatch(version):
            errors.append(f"CHANGELOG.md: non-SemVer release {version!r}")
        if not date.fullmatch(release_date):
            errors.append(
                f"CHANGELOG.md: release {version!r} needs YYYY-MM-DD date"
            )
    return errors


def check_v01_traceability() -> list[str]:
    scope_ids = set(
        re.findall(
            STORY_FILES[ROOT / "docs" / "v0.1-scope.md"],
            (ROOT / "docs" / "v0.1-scope.md").read_text(encoding="utf-8"),
        )
    )
    coverage_ids = set(
        re.findall(r"V01-[A-Z]+-\d+", COVERAGE_FILE.read_text(encoding="utf-8"))
    )
    errors = [
        f"docs/endpoint-coverage.md: missing scoped story {story_id}"
        for story_id in sorted(scope_ids - coverage_ids)
    ]
    implementation_ids = set(
        re.findall(
            r"V01-[A-Z]+-\d+", IMPLEMENTATION_FILE.read_text(encoding="utf-8")
        )
    )
    errors.extend(
        f"docs/implementation-plan.md: missing scoped story {story_id}"
        for story_id in sorted(scope_ids - implementation_ids)
    )
    return errors


def main() -> int:
    errors = [
        *check_local_links(),
        *check_stories(),
        *check_changelog(),
        *check_v01_traceability(),
    ]
    if errors:
        for error in errors:
            print(f"error: {error}")
        return 1
    story_count = sum(
        1
        for source, pattern in STORY_FILES.items()
        for line in source.read_text(encoding="utf-8").splitlines()
        if re.match(rf"^- \*\*{pattern}\*\*", line)
    )
    print(f"documentation checks passed ({len(MARKDOWN_FILES)} files, {story_count} stories)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
