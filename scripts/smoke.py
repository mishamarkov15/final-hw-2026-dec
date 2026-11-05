#!/usr/bin/env python3
"""Exercise a running CashCraft stack using isolated, newly registered accounts."""

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
import csv
from decimal import Decimal
import http.client
import io
import json
import os
import secrets
import sys
import urllib.error
import urllib.request
import uuid


class CheckFailure(Exception):
    pass


def check(condition, message):
    if not condition:
        raise CheckFailure(message)


class API:
    def __init__(self, base_url):
        self.base_url = base_url.rstrip("/")

    def request(self, method, path, data=None, token=None, expected=200,
                content_type="application/json", raw=False, headers=None):
        request_headers = dict(headers or {})
        if token:
            request_headers["Authorization"] = "Bearer " + token
        body = data
        if data is not None:
            if not isinstance(data, (str, bytes)):
                body = json.dumps(data, ensure_ascii=False).encode("utf-8")
            elif isinstance(data, str):
                body = data.encode("utf-8")
            request_headers["Content-Type"] = content_type
        req = urllib.request.Request(self.base_url + path, data=body,
                                     headers=request_headers, method=method)
        try:
            response = urllib.request.urlopen(req, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status = response.status
            result = response.read()
            response_headers = response.headers
        if expected is not None:
            check(status == expected,
                  f"{method} {path}: expected HTTP {expected}, got {status}")
        if raw:
            return status, result.decode("utf-8"), response_headers
        try:
            return status, json.loads(result), response_headers
        except (ValueError, UnicodeDecodeError):
            raise CheckFailure(f"{method} {path}: expected valid JSON") from None


def money(value):
    return Decimal(str(value))


def run(base_url, skip_large_bodies=False):
    api = API(base_url)
    run_id = uuid.uuid4().hex
    password = secrets.token_urlsafe(24)
    email_a = f"smoke-a-{run_id}@example.com"
    email_b = f"smoke-b-{run_id}@example.com"
    passed = 0

    def ok(name):
        nonlocal passed
        passed += 1
        print(f"PASS {name}", flush=True)

    def get(path, token):
        return api.request("GET", path, token=token)[1]

    def post(path, body, token, expected=201):
        return api.request("POST", path, body, token, expected=expected)[1]

    def budget(token, category, limit):
        return post("/api/budgets", {"category": category, "limit": limit}, token)

    def transaction(token, amount, category, date="2026-01-15", description="smoke", expected=201):
        return post("/api/transactions", {"amount": amount, "category": category,
                    "description": description, "date": date}, token, expected)

    report_path = "/api/reports/summary?from=2026-01-01&to=2026-01-31"
    _, ping, _ = api.request("GET", "/ping", raw=True)
    check(ping == "pong", "ping response")
    _, specification, _ = api.request("GET", "/openapi.yaml", raw=True)
    check("openapi:" in specification, "OpenAPI is available")
    api.request("GET", "/docs", raw=True)
    ok("public health and API documentation")

    api.request("POST", "/api/auth/register", {"email": "invalid", "password": password}, expected=400)
    api.request("POST", "/api/auth/register", {"email": email_a, "password": "short"}, expected=400)
    a = api.request("POST", "/api/auth/register", {"email": email_a, "password": password}, expected=201)[1]
    b = api.request("POST", "/api/auth/register", {"email": email_b, "password": password}, expected=201)[1]
    token_a, token_b = a["access_token"], b["access_token"]
    check(a["user"]["id"] != b["user"]["id"], "different account identifiers")
    check(a["token_type"] == "Bearer" and isinstance(a["expires_at"], int), "auth response schema")
    check("password" not in a["user"] and "password_hash" not in a["user"], "no password in response")
    api.request("POST", "/api/auth/register", {"email": email_a.upper(), "password": password}, expected=409)
    wrong = api.request("POST", "/api/auth/login", {"email": email_a, "password": "wrong-password"}, expected=401)[1]
    unknown = api.request("POST", "/api/auth/login", {"email": "missing-" + email_a, "password": "wrong-password"}, expected=401)[1]
    check(wrong == unknown == {"error": "invalid email or password"}, "generic failed login")
    login = api.request("POST", "/api/auth/login", {"email": "  " + email_a.upper() + "  ", "password": password})[1]
    check(login["user"] == a["user"], "normalized email login")
    token_a = login["access_token"]
    ok("registration, duplicate email, bcrypt login and generic credential errors")

    for path in ["/api/budgets", "/api/transactions", report_path,
                 "/api/transactions/import", "/api/transactions/bulk", "/api/transactions/export.csv"]:
        api.request("GET", path, expected=401)
        api.request("GET", path, token="invalid-token", expected=401)
    token_parts = token_a.split(".")
    token_parts[2] = ("A" if token_parts[2][0] != "A" else "B") + token_parts[2][1:]
    api.request("GET", "/api/budgets", token=".".join(token_parts), expected=401)
    none_header = base64.urlsafe_b64encode(b'{"alg":"none","typ":"JWT"}').decode().rstrip("=")
    api.request("GET", "/api/budgets", token=none_header + "." + token_a.split(".")[1] + ".", expected=401)
    for token in (token_a, token_b):
        check(get("/api/budgets", token) == [], "empty budget list is []")
        check(get("/api/transactions", token) == [], "empty transaction list is []")
    ok("missing, forged and unsigned JWT rejection; empty arrays")

    budget(token_a, "  еда  ", 100)
    check(get("/api/budgets", token_a) == [{"category": "еда", "limit": 100}], "normalized budget")
    get("/api/budgets", token_a)
    budget(token_a, "еда", 120)
    check(get("/api/budgets", token_a)[0]["limit"] == 120, "budget cache invalidation")
    budget(token_a, "еда", 100)
    transaction(token_a, 60, "еда", date="2026-01-01")
    transaction(token_a, 40, "еда", date="2026-01-31")
    transaction(token_a, 0.01, "еда", expected=409)
    check(len(get("/api/transactions", token_a)) == 2, "over-budget transaction was not stored")
    budget(token_a, "копейки", 0.30)
    transaction(token_a, 0.10, "копейки")
    transaction(token_a, 0.20, "копейки")
    transaction(token_a, 0.01, "копейки", expected=409)
    ok("budget updates, cache invalidation, exact limit and decimal cents")

    budget(token_a, "parallel", 5)

    def concurrent_add(_):
        body = {"amount": 1, "category": "parallel", "description": "parallel smoke", "date": "2026-01-15"}
        return api.request("POST", "/api/transactions", body, token_a, expected=None)[0]

    with ThreadPoolExecutor(max_workers=12) as pool:
        statuses = list(pool.map(concurrent_add, range(12)))
    check(statuses.count(201) == 5 and statuses.count(409) == 7,
          "parallel category writes must accept exactly five transactions")
    parallel = [tx for tx in get("/api/transactions", token_a) if tx["category"] == "parallel"]
    check(len(parallel) == 5 and sum(money(tx["amount"]) for tx in parallel) == 5,
          "concurrent writes preserve the budget")
    ok("12 concurrent writes cannot exceed the category budget")

    check(get("/api/budgets", token_b) == [], "second user cannot read budgets")
    check(get("/api/transactions", token_b) == [], "second user cannot read transactions")
    budget(token_b, "еда", 20)
    transaction(token_b, 15, "еда")
    transaction(token_b, 5.01, "еда", expected=409)
    check(get("/api/budgets", token_b) == [{"category": "еда", "limit": 20}], "per-user budget cache")
    check(len(get("/api/transactions", token_b)) == 1, "per-user transaction query")
    ignored_identity = api.request("GET", "/api/transactions", token=token_b,
                                   headers={"X-User-ID": str(a["user"]["id"])})[1]
    check(ignored_identity == get("/api/transactions", token_b), "client headers cannot choose another identity")
    ok("SQL and cache isolation between two users")

    budget(token_a, "unused", 200)
    transaction(token_a, 7.75, "без бюджета", date="2026-01-16")
    transaction(token_a, 9.25, "без бюджета", date="2025-12-31")
    report = get(report_path, token_a)
    check(money(report["total"]) == Decimal("113.05"), "inclusive January report total")
    categories = {row["category"]: row for row in report["categories"]}
    check(categories["еда"]["budget_used_percent"] == 100, "budget usage percentage")
    check(categories["unused"]["total"] == 0 and categories["unused"]["has_budget"], "budget-only category in report")
    check(not categories["без бюджета"]["has_budget"], "unbudgeted category in report")
    check(get(report_path, token_a) == report, "repeated cached report")
    check(get(report_path, token_b)["total"] == 15, "per-user report cache")
    transaction(token_a, 2, "без бюджета")
    check(money(get(report_path, token_a)["total"]) == Decimal("115.05"), "report invalidation after transaction")
    budget(token_a, "еда", 200)
    changed = {row["category"]: row for row in get(report_path, token_a)["categories"]}
    check(changed["еда"]["budget_used_percent"] == 50, "report invalidation after budget update")
    ok("reports, inclusive dates, zero spending, percentages and cache invalidation")

    count_before = len(get("/api/transactions", token_a))
    for amount, category, date in [(0, "x", "2026-01-01"), (-1, "x", "2026-01-01"),
                                   (1, "", "2026-01-01"), (1, "x", "2026-02-30"),
                                   (0.001, "x", "2026-01-01")]:
        transaction(token_a, amount, category, date=date, expected=400)
    transaction(token_a, 1, "x", description="x" * 2001, expected=400)
    post("/api/budgets", {"category": "еда", "limit": -1}, token_a, expected=400)
    post("/api/budgets", {"category": "еда", "limit": 5, "unknown": True}, token_a, expected=400)
    api.request("POST", "/api/budgets", '{} {}', token_a, expected=400)
    api.request("GET", "/api/reports/summary?from=2026-02-01&to=2026-01-01", token=token_a, expected=400)
    api.request("DELETE", "/api/budgets", token=token_a, expected=405)
    check(len(get("/api/transactions", token_a)) == count_before, "invalid transactions do not persist")
    ok("validation, strict JSON, date ranges and unsupported methods")

    csv_buffer = io.StringIO(newline="")
    writer = csv.writer(csv_buffer, lineterminator="\n")
    writer.writerow(["amount", "category", "description", "date"])
    multiline_description = 'обед, "кафе"\nвторая строка'
    writer.writerow([10, "csv", multiline_description, "2026-01-10"])
    writer.writerow([-1, "csv", "invalid negative", "2026-01-10"])
    writer.writerow([20, "csv", "valid", "2026-01-11"])
    writer.writerow(["not-a-number", "csv", "invalid number", "2026-01-12"])
    imported = api.request("POST", "/api/transactions/import", csv_buffer.getvalue(), token_a,
                           content_type="text/csv")[1]
    check(imported["total"] == 4 and imported["created"] == 2, "best-effort CSV import counts")
    check([item["row"] for item in imported["errors"]] == [4, 6], "CSV error physical line numbers")
    check(len(get("/api/transactions", token_a)) == count_before + 2, "CSV persists only valid rows")
    before_malformed = len(get("/api/transactions", token_a))
    api.request("POST", "/api/transactions/import",
                'amount,category,description,date\n1,csv,first,2026-01-01\n"unterminated',
                token_a, expected=400, content_type="text/csv")
    check(len(get("/api/transactions", token_a)) == before_malformed, "malformed CSV leaves no partial import")
    ok("CSV import, quoted Unicode/multiline fields, row errors and malformed-document rejection")

    budget(token_a, "bulk", 10)
    bulk_transactions = [{"amount": amount, "category": "bulk", "description": "bulk smoke", "date": "2026-01-15"}
                         for amount in [3, -1, 20, 4]]
    bulk = post("/api/transactions/bulk", {"transactions": bulk_transactions}, token_a, expected=200)
    check(bulk["total"] == 4 and bulk["created"] == 2, "bulk best-effort counts")
    check([item["row"] for item in bulk["errors"]] == [2, 3], "bulk row errors are ordered")
    empty = post("/api/transactions/bulk", {"transactions": []}, token_a, expected=200)
    check(empty == {"total": 0, "created": 0, "errors": []}, "empty bulk result")
    ok("bulk processing and deterministic row errors")

    _, exported, headers = api.request("GET", "/api/transactions/export.csv", token=token_a, raw=True)
    check("text/csv" in headers.get("Content-Type", ""), "CSV response type")
    check("attachment" in headers.get("Content-Disposition", ""), "CSV download disposition")
    reader = csv.DictReader(io.StringIO(exported, newline=""))
    rows = list(reader)
    check(reader.fieldnames == ["amount", "category", "description", "date"], "export column order")
    check(len(rows) == len(get("/api/transactions", token_a)), "export contains all user transactions")
    check(any(row["description"] == multiline_description for row in rows), "CSV quote and newline round trip")
    _, second_export, _ = api.request("GET", "/api/transactions/export.csv", token=token_b, raw=True)
    check(len(list(csv.DictReader(io.StringIO(second_export)))) == 1, "CSV export is user-scoped")
    ok("CSV export and per-user isolation")

    count_before_limits = len(get("/api/transactions", token_a))
    too_many = [{"amount": 1, "category": "over-limit", "description": "", "date": "2026-01-01"}] * 1001
    post("/api/transactions/bulk", {"transactions": too_many}, token_a, expected=400)
    csv_too_many = "amount,category,description,date\n" + "1,over-limit,,2026-01-01\n" * 1001
    api.request("POST", "/api/transactions/import", csv_too_many, token_a, expected=400, content_type="text/csv")
    if not skip_large_bodies:
        api.request("POST", "/api/budgets", '{"category":"' + "x" * (2 << 20) + '"}', token_a, expected=413)
        api.request("POST", "/api/transactions/import", "x" * ((10 << 20) + 1), token_a,
                    expected=413, content_type="text/csv")
    check(len(get("/api/transactions", token_a)) == count_before_limits, "oversized imports do not persist")
    ok("1000-row import limits" if skip_large_bodies else "JSON/CSV size limits and 1000-row import limits")
    if skip_large_bodies:
        print("SKIP oversized-body uploads; run without --skip-large-bodies against the local stack.")

    print(f"All {passed} smoke scenarios passed. Two isolated test accounts were created.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default=os.environ.get("CASHCRAFT_BASE_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--skip-large-bodies", action="store_true",
                        help="skip oversized 2/10 MiB uploads when checking a bandwidth-limited public tunnel")
    args = parser.parse_args()
    try:
        run(args.base_url, args.skip_large_bodies)
    except (CheckFailure, urllib.error.URLError, http.client.HTTPException, OSError, KeyError, TypeError) as error:
        print(f"FAIL {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
