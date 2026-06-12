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
"""

import argparse
import csv
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "save_migrate"))
from migrate_saves import collect_rows  # noqa: E402

TRACKED = ("name", "race", "gender")


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


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--saves-dir", required=True, help="directory containing account_*_*_pdr.bin files")
    parser.add_argument("--tool", default="../ryzomcore/build/ryzom/bin/Debug/save_to_json", help="path to the save_to_json bridge binary")
    parser.add_argument("--sheet-id-path", default="../ryzomcore/build/ryzom/bin/Debug", help="directory containing sheet_id.bin")
    parser.add_argument("--db-url", help="PostgreSQL DSN (psycopg2)")
    parser.add_argument("--rows-csv", help="CSV export of the characters table (see module docstring)")
    args = parser.parse_args()

    if bool(args.db_url) == bool(args.rows_csv):
        parser.error("exactly one of --db-url or --rows-csv is required")
    if not Path(args.tool).is_file():
        parser.error(f"bridge tool not found: {args.tool}")

    save_rows, total, failures = collect_rows(args)
    db_rows = db_rows_from_url(args.db_url) if args.db_url else db_rows_from_csv(args.rows_csv)

    mismatches = 0
    for save in save_rows:
        key = (save["account_id"], save["slot"])
        db = db_rows.get(key)
        if db is None:
            print(f"MISMATCH account {key[0]} slot {key[1]}: no PostgreSQL row")
            mismatches += 1
            continue
        for field in TRACKED:
            if str(save[field]) != str(db[field]):
                print(f"MISMATCH account {key[0]} slot {key[1]} field {field}: "
                      f"save={save[field]!r} db={db[field]!r}")
                mismatches += 1

    print(f"{len(save_rows)} save files checked against {len(db_rows)} db rows: "
          f"{mismatches} mismatch(es), {failures} unreadable file(s)", file=sys.stderr)
    return 1 if (mismatches or failures) else 0


if __name__ == "__main__":
    sys.exit(main())
