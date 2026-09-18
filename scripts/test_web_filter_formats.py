#!/usr/bin/env python3
"""Bounded anonymous checks of observed web filter encodings, not every combination.

Run after audit_web_filters.py; uses its local category captures to validate all
field names before making requests. Requires written permission and --live.
Do not run concurrently with another live probe. No cookies or mutations.
"""

import argparse
import json
import re
import urllib.parse
from decimal import Decimal

from probe_web import CACHE, Page, fetch
from test_web_anonymous import results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--live", action="store_true")
    mode.add_argument("--cached", action="store_true", help="recheck existing captures without any network access")
    args = parser.parse_args()
    checks = []

    def check(name, passed, **evidence):
        result = {"name": name, "passed": bool(passed), **evidence}
        checks.append(result)
        report = "filter-format-cached-results.json" if args.cached else "filter-format-results.json"
        (CACHE / report).write_text(json.dumps(checks, indent=2) + "\n")
        print(json.dumps(result), flush=True)
        if not passed:
            raise AssertionError(name)

    def submit(category, name, parameters):
        page = Page((CACHE / f"category-{category}.html").read_text())
        form = page.props_with("searchUrl")[0]
        names = {field["name"] for field in page.fields}
        if any(key not in names for key, _ in parameters):
            raise ValueError("parameter not observed in category form")
        url = form["searchUrl"] + "?" + urllib.parse.urlencode([("categoryId", category), *parameters])
        if args.cached:
            body, evidence = (CACHE / (name + ".html")).read_text(), {"source": "local capture"}
        else:
            body, evidence = fetch(url, name + ".html")
        return Page(body), evidence

    def bounds(page, key, minimum, maximum):
        item = next(p["rangeItem"] for p in page.props_with("rangeItem") if p["rangeItem"]["attributeId"] == key)
        return str(item.get("min", "")) == minimum and str(item.get("max", "")) == maximum

    cars, evidence = submit("216", "formats-cars", [
        ("attributeMap[autos.km_i]", "10000"), ("attributeMap[autos.km_i]", "50000"),
        ("attributeMap[autos.ez_i]", "2020"), ("attributeMap[autos.ez_i]", "2025"),
        ("attributeMap[autos.tuevy_i]", "2027"), ("attributeMap[autos.tuevy_i]", ""),
        ("clickableOptions", "autos.air_conditioning_b"), ("clickableOptions", "autos.navi_b"),
    ])
    state = cars.props_with("searchUrl")[0]
    check("INT range", bounds(cars, "autos.km_i", "10000", "50000"), request=evidence)
    check("UNFORMATTED_INT range", bounds(cars, "autos.ez_i", "2020", "2025"))
    check("multiple boolean options", {"autos.air_conditioning_b", "autos.navi_b"} <= set(state["clickableOptions"]))
    suggestion = next(p["item"] for p in cars.props_with("item") if p["item"].get("attributeId") == "autos.tuevy_i")
    check("single suggestion lower bound", str(suggestion.get("min", "")) == "2027")
    car_rows = results(cars)
    kms = [int(match[1].replace(".", "")) for row in car_rows for value in row["attributes"]
           if (match := re.fullmatch(r"([\d.]+) km", value))]
    years = [int(match[1]) for row in car_rows for value in row["attributes"]
             if (match := re.fullmatch(r"EZ \d{2}/(\d{4})", value))]
    check("returned mileage bounds", len(kms) == len(car_rows) > 0 and all(10000 <= value <= 50000 for value in kms), sampled=len(kms))
    check("returned registration bounds", len(years) == len(car_rows) > 0 and all(2020 <= value <= 2025 for value in years), sampled=len(years))

    flats, evidence = submit("203", "formats-flats", [
        ("attributeMap[wohnung_mieten.zimmer_d]", "1.5"), ("attributeMap[wohnung_mieten.zimmer_d]", "3.5"),
        ("attributeMap[wohnung_mieten.verfuegbarm_i]", "10"), ("attributeMap[wohnung_mieten.verfuegbarm_i]", ""),
        ("attributeMap[wohnung_mieten.verfuegbary_i]", "2026"), ("attributeMap[wohnung_mieten.verfuegbary_i]", ""),
    ])
    check("DECIMAL range", bounds(flats, "wohnung_mieten.zimmer_d", "1.5", "3.5"), request=evidence)
    month_year = flats.props_with("month")[0]
    check("month/year pair", str(month_year["month"].get("min", "")) == "10" and str(month_year["year"].get("min", "")) == "2026")
    flat_rows = results(flats)
    rooms = [Decimal(match[1].replace(",", ".")) for row in flat_rows for value in row["attributes"]
             if (match := re.fullmatch(r"([\d.,]+) Zi\.", value))]
    check("returned room bounds", len(rooms) == len(flat_rows) > 0 and all(Decimal("1.5") <= value <= Decimal("3.5") for value in rooms), sampled=len(rooms))

    # Removing inputs must remove their state, not reuse a prior search's values.
    cleared, evidence = submit("203", "formats-cleared", [])
    state = cleared.props_with("searchUrl")[0]
    check("clear category filters", not state["attributeMap"] and not state["clickableOptions"], request=evidence)


if __name__ == "__main__":
    main()
