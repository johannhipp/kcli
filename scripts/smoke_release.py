#!/usr/bin/env python3
"""Smoke a packaged kcli binary without account credentials or network calls."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve()
    with tempfile.TemporaryDirectory(prefix="kcli-release-") as scratch:
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(("KCLI_", "KLEINANZEIGEN_"))}
        for kind in ("CONFIG", "STATE", "CACHE"):
            env[f"KCLI_{kind}_HOME"] = str(Path(scratch) / kind.lower())

        def run(*argv, code=0, structured=True):
            result = subprocess.run([str(binary), *argv], cwd=scratch, env=env,
                                    input="", capture_output=True, text=True, timeout=15)
            if result.returncode != code:
                raise AssertionError(f"{argv}: exit {result.returncode}, expected {code}")
            if structured:
                return json.loads(result.stdout)
            if not result.stdout.strip():
                raise AssertionError(f"{argv}: empty stdout")
            return result.stdout

        version = run("version")["data"]
        operations = run("schema", "list")["data"]
        assert len(operations) == 23, "unexpected command count"
        for operation in operations:
            path = operation["path"].split()
            run(*path, "--help", structured=False)
            schema = run("schema", "show", *path)["data"]
            assert schema["operation"]["path"] == operation["path"]
            assert schema["input_schema"] and schema["output_schema"]
        for shell in ("bash", "zsh", "fish"):
            run("completion", shell, structured=False)
        error = run("listing", "get", "../invalid", code=2)
        assert error["schema"] == "kcli.error/v1" and not error["retryable"]
        checks = run("doctor")["data"]
        assert next(x for x in checks if x["name"] == "state")["status"] == "ok"
        assert next(x for x in checks if x["name"] == "network")["status"] == "skipped"
        print(json.dumps({"platform": version["os"] + "/" + version["arch"],
                          "version": version["version"], "commands_checked": len(operations),
                          "schema_help_completion_state_errors": "pass"}))


if __name__ == "__main__":
    main()
