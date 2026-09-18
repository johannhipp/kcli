#!/usr/bin/env python3
"""Manually invoked web acceptance: python3 scripts/test_web_anonymous.py --live.

Requires written permission. No account cookies, logins, or mutations. Stops on
access/rate failures. Reports safe counts and contract fields, never user prose.
This tests web contracts; it does not prove that the Go CLI implements them.
"""

import argparse
import json
import time
import urllib.parse
import urllib.request
import urllib.error
from html.parser import HTMLParser

from probe_web import CACHE, HOST, NoRedirect, Page, fetch


def results(page):
    groups = page.props_with("resultAds")
    return [row["organicAdPreview"] for row in groups[0]["resultAds"]
            if row.get("organicAdPreview")] if groups else []


def categories(items):
    return [item for parent in items for item in [parent, *categories(parent.get("children", []))]]


class Detail(HTMLParser):
    def __init__(self, body):
        super().__init__(convert_charrefs=True)
        self.ids = set()
        self.images = set()
        self.feed(body)

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if attrs.get("id"):
            self.ids.add(attrs["id"])
        if attrs.get("data-imgsrc"):
            self.images.add(attrs["data-imgsrc"])


def image_check(detail):
    image_url = sorted(detail.images)[0]
    parsed = urllib.parse.urlsplit(image_url)
    if parsed.scheme != "https" or parsed.netloc != "img.kleinanzeigen.de":
        raise ValueError("unrecognized image host")
    time.sleep(2.75)
    opener = urllib.request.build_opener(NoRedirect())
    try:
        with opener.open(image_url, timeout=25) as response:
            data = response.read(25 * 1024 * 1024 + 1)
            mime = response.headers.get("Content-Type", "")
            valid = response.code == 200 and len(data) <= 25 * 1024 * 1024 and mime.startswith("image/") and (data.startswith((b"\xff\xd8\xff", b"\x89PNG\r\n\x1a\n")) or (data[:4] == b"RIFF" and data[8:12] == b"WEBP"))
            return {"name": "image download", "passed": valid, "bytes":len(data), "mime":mime}
    except urllib.error.HTTPError as error:
        # Volatility is expected, but a 404 is not evidence of a working image.
        return {"name": "image download", "passed":False, "status":error.code, "retried":False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true", required=True)
    parser.add_argument("--image-only", metavar="CAPTURE", help="check one image from an existing local detail capture")
    args = parser.parse_args()
    if args.image_only:
        if "/" in args.image_only or "\\" in args.image_only:
            parser.error("capture must be a basename")
        result = image_check(Detail((CACHE / args.image_only).read_text()))
        print(json.dumps(result))
        (CACHE / "image-result.json").write_text(json.dumps(result, indent=2) + "\n")
        return 0 if result["passed"] else 1
    report = {"backend": "public-web", "checks": [], "requests": []}

    def load(name, url):
        body, evidence = fetch(url, name)
        report["requests"].append(evidence)
        return body, Page(body)

    def check(name, passed, **evidence):
        report["checks"].append({"name": name, "passed": bool(passed), **evidence})
        print(json.dumps(report["checks"][-1]), flush=True)
        if not passed:
            raise AssertionError(name)

    try:
        body, home = load("acceptance-home.html", HOST + "/")
        form = home.props_with("searchUrl")[0]
        tree = categories(form["categories"])
        bike = next(x for x in tree if x["categoryName"] == "Fahrräder & Zubehör")
        check("category tree", len(tree) > 100, count=len(tree), bicycle_category=bike["id"])
        for term in ("Berlin", "10115"):
            body, _ = load("location-" + term + ".json", form["locationSuggestionsUrl"] + "?" + urllib.parse.urlencode({"query": term}))
            locations = json.loads(body)
            check("location " + term, len(locations) > 1, count=len(locations))
        _, search = load("acceptance-search.html", form["searchUrl"] + "?" + urllib.parse.urlencode({"keywords": "Fahrrad", "categoryId": bike["id"], "locationId": "3331", "radius": "10", "minPrice": "50", "maxPrice": "200", "adType": "OFFER", "sortingField": "PRICE_AMOUNT"}))
        active = search.props_with("searchUrl")[0]
        check("combined filters echoed", all(active[k] == v for k,v in {"categoryId": bike["id"], "locationId":3331, "distanceRadius":10, "minPrice":"50", "maxPrice":"200", "adType":"OFFER", "sortingField":"PRICE_AMOUNT"}.items()))
        rows = results(search)
        check("structured search data", len(rows) > 0 and all(row.get("id") and row.get("seoLink") for row in rows), count=len(rows), fields=sorted(rows[0]))
        options = search.props_with("availableOptions")[0]["availableOptions"]
        check("four sort modes", {x["value"] for x in options} == {"SORTING_DATE", "PRICE_AMOUNT", "PRICE_AMOUNT_DESC", "DISTANCE"})
        for sort in ("PRICE_AMOUNT_DESC", "DISTANCE", "SORTING_DATE"):
            _, sorted_page = load("sort-"+sort+".html", form["searchUrl"] + "?" + urllib.parse.urlencode({"keywords":"Fahrrad", "categoryId":bike["id"], "locationId":3331, "radius":10, "sortingField":sort}))
            check("sort " + sort, sorted_page.props_with("searchUrl")[0]["sortingField"] == sort)
        dynamic = next(link for link in search.links if "+fahrraeder.type_s:" in link)
        _, filtered = load("acceptance-dynamic.html", HOST + dynamic)
        filter_state = filtered.props_with("searchUrl")[0]
        check("category filter retained", bool(filter_state["attributeMap"] or filter_state["nonRangeAttributeMap"]), attribute_keys=sorted({*filter_state["attributeMap"], *filter_state["nonRangeAttributeMap"]}))
        next_url = next(link for link in search.links if "/seite:2/" in link)
        _, second = load("acceptance-page2.html", HOST + next_url)
        ids1, ids2 = {r["id"] for r in rows}, {r["id"] for r in results(second)}
        check("pagination", bool(ids2 - ids1), first=len(ids1), second=len(ids2), duplicates=len(ids1 & ids2))
        wanted = next(link for link in search.links if "/anzeige:gesuche/" in link)
        _, wanted_page = load("acceptance-wanted.html", HOST + wanted)
        check("wanted ads", wanted_page.props_with("searchUrl")[0]["adType"] == "WANTED")
        listing = next(link for link in search.links if link.startswith("/s-anzeige/"))
        body, page = load("acceptance-detail.html", HOST + listing)
        detail = Detail(body)
        required = {"viewad-title", "viewad-price", "viewad-locality", "viewad-description-text", "viewad-contact", "viewad-details"}
        check("listing details", required <= detail.ids, sections=sorted(required))
        check("gallery variants", len(detail.images) > 0, variants=len(detail.images))
        seller = next(link for link in page.links if link.startswith("/s-bestandsliste.html?userId="))
        _, seller_page = load("acceptance-seller.html", HOST + seller)
        check("public seller inventory", any(link.startswith("/s-anzeige/") for link in seller_page.links))
        image_result = image_check(detail)
        check(**image_result)
    finally:
        (CACHE / "acceptance-results.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
