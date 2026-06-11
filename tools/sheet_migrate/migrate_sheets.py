#!/usr/bin/env python3
"""Sheet data -> PostgreSQL migration (Phase 4, Task 4.1c — sbrick slice).

Currently imports the 625 .sbrick files that ship in ryzomcore_leveldesign
under game_element/sbrick/ into the `bricks` table. The .item / .creature
import requires the full game installation (those data files are NOT in the
leveldesign checkout) and will be added here when that install is available.

Georges FORM facts (verified against the corpus, 2026-06-11):
  - Each .sbrick is a <FORM> whose first <STRUCT> is the sheet body; ATOMs
    and named STRUCTs (Basics, Client, faber, Create, Mandatory, Optional,
    Parameter, Credit) nest beneath it.
  - brick_type comes from Basics.FamilyId classified through the TBrickFamily
    enum ranges in game_share/brick_families.h — exactly what the C++ client's
    CSBrickSheet::isRoot()/isCredit()/... do. The Mandatory/Optional/
    Parameter/Credit STRUCTs on a brick are NOT its type: they are the family
    lists of bricks allowed to attach to it (root bricks carry them); they are
    preserved in extras for the phrase validator. Directory placement is only
    a fallback for FamilyIds missing from the enum.
  - "SPCost" (476 files) is the skill-point cost. HP/SAP/STA costs appear as
    "Property N" atoms of the form "SAP:10" (rare).
  - "LearnRequiresOneOfSkills" is "<SKILL> <level>", e.g. "SF 0".

Everything not promoted to a real column is preserved in `extras` JSONB —
nothing is dropped. Files that fail to parse are logged to
migration_warnings.log (plan rule: never silently drop).

Usage:
  migrate_sheets.py [--sheets PATH] [--out-sql FILE]   # emit SQL
  migrate_sheets.py --db-url postgres://...            # direct insert (psycopg2)
"""

import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

DEFAULT_SHEETS = (Path(__file__).resolve().parents[3]
                  / "ryzomcore_leveldesign/game_element/sbrick")
DEFAULT_FAMILIES_H = (Path(__file__).resolve().parents[3]
                      / "ryzomcore/ryzom/common/src/game_share/brick_families.h")

FAMILY_BY_DIR = {
    "fight": "Combat",
    "magic": "Magic",
    "craft": "Craft",
    "forage": "Forage",
    "harvest": "Forage",
    "enchantment": "Enchantment",
    "interface": "Interface",
    "timed_action": "TimedAction",
}
TYPE_BY_SUBDIR = {"root": "Root", "credit": "Credit", "optional": "Optional"}
COST_KEYS = {"SAP": "sap_cost", "HP": "hp_cost", "STA": "sta_cost"}


class BrickFamilies:
    """TBrickFamily enum from brick_families.h, with the same range
    classification as BRICK_FAMILIES::isRootFamily()/isCreditFamily()/etc.
    Sentinel ranges aliased to AutoCodeCheck never match a real family."""

    # (type, range-name-prefix) pairs mirroring the is*Family() functions; the
    # check order matters only for safety — ranges are disjoint in the enum.
    RANGE_GROUPS = [
        ("Root", ["CombatRoot", "MagicRoot", "FaberRoot", "HarvestRoot",
                  "ForageProspectionRoot", "ForageExtractionRoot", "PowerRoot",
                  "ProcEnchantement"]),
        ("Mandatory", ["CombatMandatory", "MagicMandatory", "FaberMandatory",
                       "HarvestMandatory", "ForageProspectionMandatory",
                       "ForageExtractionMandatory", "PowerMandatory"]),
        ("Optional", ["CombatOption", "MagicOption", "FaberOption",
                      "HarvestOption", "ForageProspectionOption",
                      "ForageExtractionOption"]),
        ("Credit", ["CombatCredit", "MagicCredit", "FaberCredit",
                    "HarvestCredit", "ForageProspectionCredit",
                    "ForageExtractionCredit", "MagicPowerCredit"]),
        ("Parameter", ["CombatParameter", "MagicParameter", "FaberParameter",
                       "HarvestParameter", "ForageProspectionParameter",
                       "ForageExtractionParameter", "PowerParameter"]),
    ]

    def __init__(self, header: Path):
        body = re.search(r"enum\s+TBrickFamily\s*\{(.*?)\};", header.read_text(), re.S)
        if not body:
            raise SystemExit(f"TBrickFamily enum not found in {header}")
        text = re.sub(r"/\*.*?\*/", "", body.group(1), flags=re.S)  # block comments
        text = re.sub(r"//[^\n]*", "", text)  # line comments
        self.values: dict[str, int] = {}
        counter = 0
        for entry in text.split(","):
            entry = entry.strip()
            if not entry:
                continue
            if "=" in entry:
                name, expr = (p.strip() for p in entry.split("=", 1))
                counter = self.values[expr] if expr in self.values else int(expr)
            else:
                name = entry
            self.values[name] = counter
            counter += 1

    def classify(self, family_id: str) -> str | None:
        v = self.values.get(family_id)
        if v is None:
            return None
        for btype, ranges in self.RANGE_GROUPS:
            for rng in ranges:
                lo = self.values.get("Begin" + rng)
                hi = self.values.get("End" + rng)
                if lo is not None and hi is not None and lo <= v <= hi:
                    return btype
        return None


def struct_to_dict(node: ET.Element) -> dict:
    out = {}
    for child in node:
        name = child.get("Name")
        if child.tag == "ATOM" and name:
            out[name] = child.get("Value", "")
        elif child.tag == "STRUCT" and name:
            sub = struct_to_dict(child)
            if sub:
                out[name] = sub
        elif child.tag == "ARRAY" and name:
            out[name] = [struct_to_dict(c) if len(c) else c.get("Value", "")
                         for c in child]
    return out


def parse_sbrick(path: Path, sheets_root: Path, warnings: list[str],
                 families: BrickFamilies | None = None) -> dict | None:
    try:
        form = ET.parse(path).getroot()
    except ET.ParseError as e:
        warnings.append(f"{path}: XML parse error: {e}")
        return None
    body = form.find("STRUCT")
    if body is None:
        warnings.append(f"{path}: no body STRUCT")
        return None
    data = struct_to_dict(body)
    basics = data.get("Basics", {})
    rel = path.relative_to(sheets_root)
    top = rel.parts[0] if len(rel.parts) > 1 else ""
    sub = rel.parts[1] if len(rel.parts) > 2 else ""

    brick_type = families.classify(basics.get("FamilyId", "")) if families else None
    if brick_type is None:
        brick_type = TYPE_BY_SUBDIR.get(sub)
        if basics.get("FamilyId"):
            warnings.append(
                f"{path}: FamilyId {basics['FamilyId']!r} not in TBrickFamily enum; "
                f"directory fallback -> {brick_type}")

    skill_req, skill_min = None, 0
    learn = basics.get("LearnRequiresOneOfSkills", "")
    m = re.match(r"\s*(\S+)\s+(\d+)", learn)
    if m:
        skill_req, skill_min = m.group(1), int(m.group(2))
    elif basics.get("Skill"):
        skill_req = basics["Skill"].split()[0]

    costs = {"sap_cost": 0, "hp_cost": 0, "sta_cost": 0}
    for k, v in basics.items():
        if k.startswith("Property") and ":" in str(v):
            prefix, _, amount = str(v).partition(":")
            col = COST_KEYS.get(prefix.strip().upper())
            if col:
                try:
                    costs[col] = int(float(amount))
                except ValueError:
                    warnings.append(f"{path}: bad {prefix} amount {amount!r}")

    extras = dict(data)
    extras["category"] = str(Path(top) / sub) if sub else top

    return {
        "id": path.stem.lower(),
        "family": FAMILY_BY_DIR.get(top, top.capitalize() or None),
        "brick_type": brick_type,
        "skill_req": skill_req,
        "skill_min": skill_min,
        **costs,
        "description": None,
        "extras": extras,
    }


def sql_literal(v) -> str:
    if v is None:
        return "NULL"
    if isinstance(v, (int, float)):
        return str(v)
    if isinstance(v, dict):
        return "'" + json.dumps(v, separators=(",", ":")).replace("'", "''") + "'::jsonb"
    return "'" + str(v).replace("'", "''") + "'"


COLS = ["id", "family", "brick_type", "skill_req", "skill_min",
        "sap_cost", "hp_cost", "sta_cost", "description", "extras"]


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--sheets", type=Path, default=DEFAULT_SHEETS)
    ap.add_argument("--families-h", type=Path, default=DEFAULT_FAMILIES_H)
    ap.add_argument("--out-sql", type=Path, default=None)
    ap.add_argument("--db-url", default=None)
    args = ap.parse_args()

    families = BrickFamilies(args.families_h)
    print(f"parsed {len(families.values)} TBrickFamily enum entries", file=sys.stderr)

    files = sorted(args.sheets.rglob("*.sbrick"))
    warnings: list[str] = []
    rows = [r for f in files if (r := parse_sbrick(f, args.sheets, warnings, families))]
    print(f"parsed {len(rows)}/{len(files)} .sbrick files", file=sys.stderr)

    if warnings:
        Path("migration_warnings.log").write_text("\n".join(warnings) + "\n")
        print(f"{len(warnings)} warnings -> migration_warnings.log", file=sys.stderr)

    if args.db_url:
        import psycopg2  # deferred: only needed for direct insert
        conn = psycopg2.connect(args.db_url)
        with conn, conn.cursor() as cur:
            cur.executemany(
                f"INSERT INTO bricks ({', '.join(COLS)}) VALUES ({', '.join('%s' for _ in COLS)})"
                " ON CONFLICT (id) DO NOTHING",
                [[json.dumps(r[c]) if c == "extras" else r[c] for c in COLS] for r in rows],
            )
        conn.close()
        print(f"inserted (or kept) {len(rows)} bricks", file=sys.stderr)
        return

    lines = [
        "-- GENERATED by tools/sheet_migrate/migrate_sheets.py — sbrick import",
        f"-- {len(rows)} bricks from {args.sheets}",
        f"INSERT INTO bricks ({', '.join(COLS)}) VALUES",
        ",\n".join("    (" + ", ".join(sql_literal(r[c]) for c in COLS) + ")" for r in rows),
        "ON CONFLICT (id) DO NOTHING;",
    ]
    sql = "\n".join(lines) + "\n"
    if args.out_sql:
        args.out_sql.write_text(sql)
        print(f"wrote {args.out_sql}", file=sys.stderr)
    else:
        print(sql)


if __name__ == "__main__":
    main()
