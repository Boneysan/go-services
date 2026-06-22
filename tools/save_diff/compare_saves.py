#!/usr/bin/env python3
"""Task 4.2c Step 2 — dual-write consistency check.

For every account_<uid>_<slot>_pdr.bin save file, deserializes the binary
(via the save_to_json bridge) and compares the tracked metadata fields
(name, race, gender) against the PostgreSQL characters row. Any mismatch is
reported with the character key and field name; the exit code is non-zero so
a cron wrapper can alert on failure.

The binary save files are authoritative during the dual-write period — a
mismatch means the EGS dual-write path (pgUpsertCharacterMetadata) and the
save file disagree and must be investigated before retiring backup_service.

Database access (one of):
  --db-url postgres://...   direct read (psycopg2)
  --rows-csv rows.csv       offline read; generate with
      docker exec ryzom-dev-postgres-1 psql -U ryzom -d ryzom_sheets -c \\
        "COPY (SELECT account_id, slot, name, race, gender FROM characters) \\
         TO STDOUT WITH CSV HEADER" > rows.csv

Streak ledger (Task 4.2c tail, automation of this previously-manual tool):
  --log-db-url postgres://...   after comparing, insert one row into
      dual_write_diff_log (works with either --db-url or --rows-csv mode;
      defaults to --db-url's connection if --log-db-url is omitted and
      --db-url was given).
  --alert-webhook-url URL        POST a Discord-style webhook payload when
      mismatch_count > 0 (also read from $DUAL_WRITE_ALERT_WEBHOOK_URL).
      If unset, a mismatch still produces a loud WARNING line on stderr.
  --streak-status --db-url ...   read-only: report the current consecutive
      zero-mismatch-day streak from dual_write_diff_log and whether the
      Task 4.2c Step 3 gate (30 real calendar days) is satisfied. This does
      NOT flip any cutover flag — that stays a human decision.
"""

import argparse
import csv
import json
import os
import sys
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "save_migrate"))
from migrate_saves import collect_rows  # noqa: E402

TRACKED = ("name", "race", "gender")
GATE_DAYS = 30


def db_rows_from_csv(path):
    rows = {}
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            rows[(row["account_id"], int(row["slot"]))] = row
    return rows


def db_rows_from_url(db_url):
    import psycopg2  # deferred: only needed for direct read
    conn = psycopg2.connect(db_url)
    rows = {}
    with conn, conn.cursor() as cur:
        cur.execute("SELECT account_id, slot, name, race, gender FROM characters")
        for account_id, slot, name, race, gender in cur.fetchall():
            rows[(account_id, slot)] = {"name": name, "race": race, "gender": gender}
    conn.close()
    return rows


def log_diff_run(log_db_url, total_compared, mismatch_count, mismatches):
    """Insert one row into dual_write_diff_log (migration 011). Best-effort:
    a logging failure must not mask a real comparison result, so callers
    should still act on the comparison's own exit code regardless."""
    import psycopg2
    conn = psycopg2.connect(log_db_url)
    with conn, conn.cursor() as cur:
        cur.execute(
            "INSERT INTO dual_write_diff_log (total_compared, mismatch_count, mismatches) "
            "VALUES (%s, %s, %s)",
            (total_compared, mismatch_count, json.dumps(mismatches)),
        )
    conn.close()


def send_alert(webhook_url, mismatch_count, mismatches):
    """POST a Discord-compatible webhook payload. Falls back to a loud
    stderr warning if no webhook is configured -- alerting is best-effort
    and must never be the reason the diff job itself fails."""
    if not webhook_url:
        print(f"WARNING: {mismatch_count} dual-write mismatch(es) detected and "
              f"no DUAL_WRITE_ALERT_WEBHOOK_URL is configured -- check logs manually.",
              file=sys.stderr)
        return
    content = (f"⚠️ Ryzom dual-write diff: {mismatch_count} mismatch(es) detected "
               f"({len(mismatches)} shown). See dual_write_diff_log / compare_saves.py output.")
    body = json.dumps({"content": content}).encode("utf-8")
    req = urllib.request.Request(
        webhook_url, data=body, headers={"Content-Type": "application/json"}, method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=10):
            pass
    except Exception as e:  # noqa: BLE001 - alerting must never crash the job
        print(f"WARNING: failed to POST dual-write alert webhook: {e}", file=sys.stderr)


def compute_streak(day_results):
    """Pure streak computation over [(date, worst_mismatch_count), ...] sorted
    most-recent-day first (one entry per calendar day). Counts consecutive
    zero-mismatch days back from the first entry; breaks on any mismatch or
    a gap in the calendar (a day with no run at all)."""
    streak = 0
    expected_day = None
    for day, worst in day_results:
        if worst != 0:
            break
        if expected_day is not None and day != expected_day:
            break
        streak += 1
        expected_day = day.fromordinal(day.toordinal() - 1)
    return streak


def streak_status(db_url):
    """Read-only: consecutive zero-mismatch days counted back from the most
    recent dual_write_diff_log row, plus whether the Step 3 gate is met.
    One run is treated as covering the calendar day it ran on; multiple runs
    on the same day collapse to one streak day via MAX(mismatch_count)."""
    import psycopg2
    conn = psycopg2.connect(db_url)
    with conn, conn.cursor() as cur:
        cur.execute(
            "SELECT checked_at::date AS day, MAX(mismatch_count) AS worst "
            "FROM dual_write_diff_log GROUP BY day ORDER BY day DESC"
        )
        days = cur.fetchall()
    conn.close()

    streak = compute_streak(days)
    return {
        "consecutive_zero_mismatch_days": streak,
        "gate_days_required": GATE_DAYS,
        "gate_satisfied": streak >= GATE_DAYS,
        "checked_at": datetime.now(timezone.utc).isoformat(),
    }


def diff_rows(save_rows, db_rows):
    """Pure comparison: save_rows is the list from collect_rows(); db_rows is
    the {(account_id, slot): {field: value}} dict from either DB source.
    Returns the list of mismatch dicts (also used as dual_write_diff_log's
    JSONB payload) and prints one MISMATCH line per finding."""
    mismatches = []
    for save in save_rows:
        key = (save["account_id"], save["slot"])
        db = db_rows.get(key)
        if db is None:
            print(f"MISMATCH account {key[0]} slot {key[1]}: no PostgreSQL row")
            mismatches.append({"account_id": key[0], "slot": key[1], "reason": "missing_db_row"})
            continue
        for field in TRACKED:
            if str(save[field]) != str(db[field]):
                print(f"MISMATCH account {key[0]} slot {key[1]} field {field}: "
                      f"save={save[field]!r} db={db[field]!r}")
                mismatches.append({
                    "account_id": key[0], "slot": key[1], "field": field,
                    "save": save[field], "db": db[field],
                })
    return mismatches


def run_comparison(args):
    save_rows, total, failures = collect_rows(args)
    db_rows = db_rows_from_url(args.db_url) if args.db_url else db_rows_from_csv(args.rows_csv)

    mismatches = diff_rows(save_rows, db_rows)

    print(f"{len(save_rows)} save files checked against {len(db_rows)} db rows: "
          f"{len(mismatches)} mismatch(es), {failures} unreadable file(s)", file=sys.stderr)
    return len(save_rows), mismatches, failures


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--saves-dir", help="directory containing account_*_*_pdr.bin files")
    parser.add_argument("--tool", default="../ryzomcore/build/ryzom/bin/Debug/save_to_json", help="path to the save_to_json bridge binary")
    parser.add_argument("--sheet-id-path", default="../ryzomcore/build/ryzom/bin/Debug", help="directory containing sheet_id.bin")
    parser.add_argument("--db-url", help="PostgreSQL DSN (psycopg2)")
    parser.add_argument("--rows-csv", help="CSV export of the characters table (see module docstring)")
    parser.add_argument("--log-db-url", help="PostgreSQL DSN to log this run into dual_write_diff_log "
                                              "(defaults to --db-url if that mode was used)")
    parser.add_argument("--alert-webhook-url", default=os.environ.get("DUAL_WRITE_ALERT_WEBHOOK_URL"),
                         help="Discord webhook URL for mismatch alerts (env DUAL_WRITE_ALERT_WEBHOOK_URL)")
    parser.add_argument("--streak-status", action="store_true",
                         help="report the current zero-mismatch streak from dual_write_diff_log and exit "
                              "(read-only; requires --db-url; does not run a comparison)")
    args = parser.parse_args()

    if args.streak_status:
        if not args.db_url:
            parser.error("--streak-status requires --db-url")
        print(json.dumps(streak_status(args.db_url), indent=2))
        return 0

    if bool(args.db_url) == bool(args.rows_csv):
        parser.error("exactly one of --db-url or --rows-csv is required")
    if not args.saves_dir:
        parser.error("--saves-dir is required")
    if not Path(args.tool).is_file():
        parser.error(f"bridge tool not found: {args.tool}")

    total_compared, mismatches, failures = run_comparison(args)

    log_url = args.log_db_url or (args.db_url if args.db_url else None)
    if log_url:
        log_diff_run(log_url, total_compared, len(mismatches), mismatches)

    if mismatches:
        send_alert(args.alert_webhook_url, len(mismatches), mismatches)

    return 1 if (mismatches or failures) else 0


if __name__ == "__main__":
    sys.exit(main())
