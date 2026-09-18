#!/usr/bin/env python3
"""Prepare a SemVer release and changelog commit; publishing belongs to CI."""
from __future__ import annotations

import datetime
import os
from pathlib import Path
import re
import subprocess

SEMVER = re.compile(r"v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)")


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def next_version(previous: str | None, messages: str) -> str:
    if previous is None:
        return "0.1.0"
    major, minor, patch = map(int, previous.removeprefix("v").split("."))
    if re.search(r"(?m)^\w+(?:\([^\n)]+\))?!:|^BREAKING[ -]CHANGE:", messages):
        return f"{major + 1}.0.0"
    if re.search(r"(?m)^feat(?:\([^\n)]+\))?:", messages):
        return f"{major}.{minor + 1}.0"
    return f"{major}.{minor}.{patch + 1}"


def update_changelog(text: str, version: str, date: str, fallback: str, repository: str) -> tuple[str, str]:
    match = re.search(r"(?ms)^## \[Unreleased\]\n(.*?)(?=^## \[|\Z)", text)
    if not match:
        raise ValueError("CHANGELOG.md must contain an Unreleased section")
    notes = re.sub(r"(?m)^\[[^\]]+\]:.*\n?", "", match[1]).strip() or fallback
    # Comparison links are maintained separately from release notes.
    text = re.sub(r"(?m)^\[[^\]]+\]:.*\n?", "", text).rstrip() + "\n"
    match = re.search(r"(?ms)^## \[Unreleased\]\n(.*?)(?=^## \[|\Z)", text)
    assert match
    text = text[:match.start()] + f"## [Unreleased]\n\n## [{version}] - {date}\n\n{notes}\n\n" + text[match.end():]
    versions = re.findall(r"(?m)^## \[(\d+\.\d+\.\d+)\]", text)
    links = [f"[Unreleased]: {repository}/compare/v{version}...HEAD"]
    for index, release in enumerate(versions):
        target = f"compare/v{versions[index + 1]}...v{release}" if index + 1 < len(versions) else f"releases/tag/v{release}"
        links.append(f"[{release}]: {repository}/{target}")
    return text.rstrip() + "\n\n" + "\n".join(links) + "\n", notes + "\n"


def output(tag: str, publish_commit: bool) -> None:
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
        stream.write(f"tag={tag}\npublish_commit={str(publish_commit).lower()}\n")


def main() -> None:
    source = git("rev-parse", "HEAD")
    tags = [tag for tag in git("tag", "--list", "v*", "--sort=-version:refname").splitlines() if SEMVER.fullmatch(tag)]
    # Rerun after a failed upload uses the exact same immutable tag and version.
    for tag in tags:
        if git("show", "-s", "--format=%P", f"{tag}^{{}}").split() == [source] and git("show", "-s", "--format=%s", f"{tag}^{{}}") == f"chore(release): {tag} [skip ci]":
            git("checkout", "--detach", tag)
            text = Path("CHANGELOG.md").read_text()
            notes = re.search(rf"(?ms)^## \[{re.escape(tag[1:])}\] - [^\n]+\n(.*?)(?=^## \[|^\[Unreleased\]:|\Z)", text)
            if not notes:
                raise ValueError("Tagged release has no matching changelog section")
            Path(os.environ["RUNNER_TEMP"], "release-notes.md").write_text(notes[1].strip() + "\n")
            output(tag, False)
            return
    previous = tags[0] if tags else None
    if previous:
        subprocess.run(["git", "merge-base", "--is-ancestor", previous, "HEAD"], check=True)
    revision = f"{previous}..HEAD" if previous else "HEAD"
    messages = git("log", "--format=%B", revision)
    version = next_version(previous, messages)
    fallback = "### Changed\n\n" + git("log", "--format=- %s (%h)", revision)
    path = Path("CHANGELOG.md")
    repository = f"https://github.com/{os.environ['GITHUB_REPOSITORY']}"
    text, notes = update_changelog(path.read_text(), version, datetime.datetime.now(datetime.timezone.utc).date().isoformat(), fallback, repository)
    path.write_text(text)
    Path(os.environ["RUNNER_TEMP"], "release-notes.md").write_text(notes)
    git("add", "CHANGELOG.md")
    git("commit", "-m", f"chore(release): v{version} [skip ci]")
    git("tag", "-a", f"v{version}", "-m", f"Release v{version}")
    output(f"v{version}", True)


if __name__ == "__main__":
    main()
