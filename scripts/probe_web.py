#!/usr/bin/env python3
"""Small anonymous web-contract probe. Never logs in or sends messages.

Use --fetch only with written permission. Raw responses stay in ignored .tmp/;
stdout reports structure, not listing prose, seller identities, or cookies.
"""

import argparse
import hashlib
import json
import random
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CACHE = ROOT / ".tmp" / "web-acceptance"
HOST = "https://www.kleinanzeigen.de"
LIMIT = 10 * 1024 * 1024


def astro(value):
    """Decode the JSON value tags actually used by Astro's public HTML."""
    if not isinstance(value, list) or not value or not isinstance(value[0], int):
        raise ValueError("unknown Astro value shape")
    tag, *rest = value
    if not rest:
        return None
    value = rest[0]
    if tag == 0:
        if isinstance(value, dict):
            return {key: astro(item) for key, item in value.items()}
        return value
    if tag == 1:
        return [astro(item) for item in value]
    if tag in (3, 6, 7):  # Date, BigInt, URL: preserve their serialized value.
        return value
    raise ValueError(f"unsupported Astro value tag: {tag}")


class Page(HTMLParser):
    def __init__(self, body):
        super().__init__(convert_charrefs=True)
        self.islands = []
        self.links = []
        self.forms = []
        self.fields = []
        self.scripts = []
        self.feed(body)

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if tag == "astro-island" and attrs.get("props"):
            raw = json.loads(attrs["props"])
            self.islands.append({
                "component": attrs.get("component-url", ""),
                "export": attrs.get("component-export", ""),
                "props": {key: astro(value) for key, value in raw.items()},
            })
        if tag == "a" and attrs.get("href"):
            self.links.append(attrs["href"])
        if tag == "form":
            self.forms.append(attrs)
        if tag in ("input", "select") and attrs.get("name"):
            self.fields.append(attrs)
        if tag == "script" and attrs.get("src"):
            self.scripts.append(attrs["src"])

    def props_with(self, key):
        return [item["props"] for item in self.islands if key in item["props"]]

    def summary(self):
        return {
            "forms": [{key: form.get(key) for key in ("action", "method")} for form in self.forms],
            "fields": sorted({field["name"] for field in self.fields}),
            "islands": [{"component": item["component"].split("/")[-1],
                         "keys": list(item["props"])} for item in self.islands],
            "listing_links": len({link for link in self.links if link.startswith("/s-anzeige/")}),
        }


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def fetch(url, name):
    CACHE.mkdir(parents=True, exist_ok=True, mode=0o700)
    opener = urllib.request.build_opener(NoRedirect())
    for _ in range(4):
        parsed = urllib.parse.urlsplit(url)
        if parsed.scheme != "https" or parsed.netloc != "www.kleinanzeigen.de":
            raise ValueError("only the exact public website host is allowed")
        stamp = CACHE / "last-request"
        previous = float(stamp.read_text()) if stamp.exists() else 0
        time.sleep(max(0, previous + 2.5 + random.uniform(0, .25) - time.time()))
        stamp.write_text(str(time.time()))
        request = urllib.request.Request(url, headers={"Accept-Language": "de-DE"})
        try:
            response = opener.open(request, timeout=25)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status = response.code
            body = response.read(LIMIT + 1)
            if len(body) > LIMIT:
                raise ValueError("response exceeds limit")
            if status in (301, 302, 303, 307, 308):
                url = urllib.parse.urljoin(url, response.headers["Location"])
                continue
            content_type = response.headers.get("Content-Type", "")
        if status in (401, 403, 429):
            raise RuntimeError(f"access/rate stop: HTTP {status}; no retry")
        lower = body.lower()
        if any(marker in lower for marker in (b"ip-eingeschraenkt", b"access denied", b"verify you are human")):
            raise RuntimeError("access/challenge stop; no retry")
        if status != 200:
            raise RuntimeError(f"HTTP {status}; no retry")
        path = CACHE / name
        path.write_bytes(body)
        path.chmod(0o600)
        return body.decode("utf-8"), {
            "status": status, "content_type": content_type,
            "observed_at": datetime.now(timezone.utc).isoformat(),
            "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(),
            "path": parsed.path,
        }
    raise RuntimeError("redirect limit reached")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("name", help="local capture basename")
    parser.add_argument("--fetch", metavar="URL", help="authorized anonymous GET")
    args = parser.parse_args()
    if Path(args.name).name != args.name:
        parser.error("name must be a basename")
    if args.fetch:
        body, result = fetch(args.fetch, args.name)
    else:
        body = (CACHE / args.name).read_text()
        result = {"source": "local capture"}
    if body.lstrip().startswith(("{", "[")):
        value = json.loads(body)
        result["json_type"] = type(value).__name__
        result["entries"] = len(value)
    else:
        result.update(Page(body).summary())
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
