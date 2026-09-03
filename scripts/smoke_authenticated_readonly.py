#!/usr/bin/env python3
"""Read-only account smoke test for monkrel/kleinanzeigen-api.

This deliberately exercises only conversations, own ads, and watchlist reads.
It never sends a message or creates, changes, renews, or deletes an ad.

Run after the package's one-time login:
  uv run --with kleinanzeigen-api==0.4.0 kleinanzeigen-api login
  uv run --with kleinanzeigen-api==0.4.0 python \
    scripts/smoke_authenticated_readonly.py --acknowledge-terms-risk
"""

from __future__ import annotations

import argparse
import json
import sys


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--acknowledge-terms-risk", action="store_true")
    args = parser.parse_args()
    if not args.acknowledge_terms_risk:
        parser.error(
            "read the repository report and pass --acknowledge-terms-risk "
            "before making authenticated automated requests"
        )
    return args


def main() -> int:
    parse_args()
    try:
        from kleinanzeigen_api import Authenticator, KleinanzeigenAPI
    except ModuleNotFoundError as exc:
        if exc.name != "kleinanzeigen_api":
            raise
        print(
            "error: optional dependency missing; run with "
            "`uv run --with kleinanzeigen-api==0.4.0 python ...`",
            file=sys.stderr,
        )
        return 2
    auth = Authenticator()
    if not auth.logged_in:
        print(
            json.dumps(
                {
                    "authenticated": False,
                    "next_step": "run: kleinanzeigen-api login",
                    "writes_attempted": False,
                },
                indent=2,
            )
        )
        return 2

    api = KleinanzeigenAPI(rate_limit=2.5, authenticator=auth)
    conversations = api.conversations()
    own_ads = api.my_ads()
    watchlist = api.watchlist()
    print(
        json.dumps(
            {
                "authenticated": True,
                "conversation_count": len(conversations),
                "own_ad_count": len(own_ads),
                "watchlist_count": len(watchlist),
                "writes_attempted": False,
            },
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
