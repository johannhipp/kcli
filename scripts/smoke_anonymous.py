#!/usr/bin/env python3
"""Conservative anonymous smoke test for monkrel/kleinanzeigen-api.

Run with:
  uv run --with kleinanzeigen-api==0.4.0 python scripts/smoke_anonymous.py \
    --acknowledge-terms-risk

This makes a small number of requests. Kleinanzeigen's terms prohibit automated
access without written permission; the explicit flag is meant to prevent this
script from being run accidentally by a scheduler.
"""

from __future__ import annotations

import argparse
import json
import sys
from urllib.request import Request, urlopen


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--query", default="ThinkPad X1")
    parser.add_argument("--location", default="Berlin")
    parser.add_argument("--distance-km", type=int, default=50)
    parser.add_argument("--size", type=int, default=5, choices=range(1, 11))
    parser.add_argument("--skip-image-check", action="store_true")
    parser.add_argument("--acknowledge-terms-risk", action="store_true")
    args = parser.parse_args()
    if not args.acknowledge_terms_risk:
        parser.error(
            "read the repository report and pass --acknowledge-terms-risk "
            "before making automated requests"
        )
    return args


def check_image(url: str) -> dict[str, object]:
    request = Request(
        url,
        headers={
            "User-Agent": "kcli-research-smoke/1.0",
            "Range": "bytes=0-1023",
        },
    )
    with urlopen(request, timeout=20) as response:
        prefix = response.read(64)
        content_type = response.headers.get_content_type()
        return {
            "reachable": bool(prefix),
            "content_type": content_type,
            "looks_like_image": content_type.startswith("image/"),
        }


def main() -> int:
    args = parse_args()
    try:
        from kleinanzeigen_api import KleinanzeigenAPI
    except ModuleNotFoundError as exc:
        if exc.name != "kleinanzeigen_api":
            raise
        print(
            "error: optional dependency missing; run with "
            "`uv run --with kleinanzeigen-api==0.4.0 python ...`",
            file=sys.stderr,
        )
        return 2
    api = KleinanzeigenAPI(rate_limit=2.5)
    listings = api.search(
        args.location,
        q=args.query,
        distance_km=args.distance_km,
        sort_type="DATE_DESCENDING",
        pages=1,
        size=args.size,
    )
    if not listings:
        raise RuntimeError("search returned no listings")

    detail = None
    detail_errors: list[str] = []
    for candidate in listings:
        try:
            detail = api.get_ad(candidate.id)
            break
        except RuntimeError as exc:
            # Listings are volatile; one can disappear between search and detail.
            detail_errors.append(f"{candidate.id}: {exc}")
    if detail is None:
        raise RuntimeError("no search result still had a readable detail page")

    image_result: dict[str, object] = {"checked": False}
    if detail.images and not args.skip_image_check:
        image_result = {"checked": True, **check_image(detail.images[0])}

    result = {
        "query": args.query,
        "location": args.location,
        "search_result_count": len(listings),
        "sample": {
            "id": detail.id,
            "title": detail.title,
            "price": detail.price,
            "city": detail.city,
            "url": detail.url,
            "description_present": bool(detail.description),
            "image_count": len(detail.images),
            "attribute_count": len(detail.attributes),
        },
        "image": image_result,
        "vanished_results": detail_errors,
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
