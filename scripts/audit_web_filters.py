#!/usr/bin/env python3
"""One bounded category walk; inventory web filter kinds without testing combinations.

Uses an already captured category tree, then anonymous form GETs at the probe's
request floor. Existing captures are reused. Requires written permission and
explicit --live; run no other live probe concurrently.
"""

import argparse
import json
import re
import urllib.parse

from probe_web import CACHE, Page, fetch
from test_web_anonymous import categories


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--live", action="store_true", required=True)
    parser.parse_args()
    home = Page((CACHE / "acceptance-home.html").read_text())
    form = home.props_with("searchUrl")[0]
    tree = {str(x["id"]): x for x in categories(form["categories"]) if x["id"]}
    report = {}
    for category_id in tree:
        name = "category-" + category_id + ".html"
        path = CACHE / name
        if path.exists():
            body = path.read_text()
        else:
            body, _ = fetch(form["searchUrl"] + "?" + urllib.parse.urlencode({"categoryId":category_id}), name)
        page = Page(body)
        selected = page.props_with("searchUrl")
        if not selected or str(selected[0]["categoryId"]) != category_id:
            raise ValueError(f"category {category_id}: search state missing or mismatched")
        keys = sorted({key for link in page.links for key in re.findall(r"\+([^:+/]+):", link)})
        ranges = [{"key": p["rangeItem"]["attributeId"], "type":p["rangeItem"]["attributeType"]} for p in page.props_with("rangeItem")]
        suggestions = [{"key": p["item"]["attributeId"], "type":p["item"]["attributeType"]}
                       for p in page.props_with("item") if isinstance(p["item"], dict) and "attributeId" in p["item"]]
        month_year = [{part: p[part]["attributeId"] for part in ("month", "year")}
                      for p in page.props_with("month") if "year" in p]
        booleans = sorted({field["value"] for field in page.fields if field.get("name") == "clickableOptions" and field.get("type") == "checkbox"})
        record = {"link_filter_keys":keys,"ranges":ranges,"suggestions":suggestions,
                  "month_year":month_year,"booleans":booleans,
                  "form_fields":sorted({field["name"] for field in page.fields})}
        report[category_id] = record
        (CACHE / "filter-audit.json").write_text(json.dumps(report, indent=2) + "\n")
        print(json.dumps({"category":category_id,"completed":len(report),"total":len(tree),"links":len(keys),"ranges":len(ranges),"booleans":len(booleans)}), flush=True)


if __name__ == "__main__":
    main()
