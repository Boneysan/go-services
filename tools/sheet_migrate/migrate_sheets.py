#!/usr/bin/env python3
"""Sheet data -> PostgreSQL migration (Phase 4, Task 4.1c).

Imports the George sheet data that ships in ryzomcore_leveldesign:
  - 625  .sbrick   game_element/sbrick/   -> bricks
  - .sitem         game_element/sitem/    -> items + item_stats
  - .creature      game_elem/creature/    -> creatures + creature_attack_lists
                                             + creature_action_configs

(The original "items/creatures need the full game install" premise was wrong
— verified 2026-06-11: 1,288 .sitem and 6,891 .creature files are in the
leveldesign checkout. Underscore-prefixed files are abstract templates that
are NOT in sheet_id.bin: they are used for PARENT resolution only and never
emitted as rows.)

Georges FORM facts (verified against the corpus, 2026-06-11):
  - Each sheet is a <FORM> whose first <STRUCT> is the sheet body; ATOMs,
    named STRUCTs and ARRAYs nest beneath it.
  - <PARENT Filename="x.sitem"/> entries (1,198/1,288 sitems, 4,443/6,891
    creatures) are George inheritance: parents merge first (in order), the
    own body overrides per-atom; arrays replace wholesale. Parent refs are
    bare filenames, resolved case-insensitively across the whole tree.
  - Enum ATOM values are .typ display LABELS ("raw material (mp)",
    "Light pants"); the DFN .typ DEFINITION elements map Label -> Value
    ("RAW_MATERIAL", "LIGHT_PANTS") which is what C++ stringToItemFamily()
    sees and what the SQL enum types use.
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
  - The creature corpus uses NO sub-DFN beyond: Basics, "3d data", Properties,
    Resists, Protections, Combat, Harvest, "Damage Shield", Collision,
    animal_bag (+ attack_listN atoms and melee/range/nuke/heal_cfg arrays).
    Loot Tables / shopkeeper / phrases columns stay at their defaults.

Everything not promoted to a real column is preserved in `extras` JSONB —
nothing is dropped. Files that fail to parse are logged to
migration_warnings.log (plan rule: never silently drop).

Usage:
  migrate_sheets.py [--only bricks,items,creatures] [--out-sql FILE]
  migrate_sheets.py --db-url postgres://...            # direct insert (psycopg2)
"""

import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

WORKSPACE = Path(__file__).resolve().parents[3]
DEFAULT_SHEETS = WORKSPACE / "ryzomcore_leveldesign/game_element/sbrick"
DEFAULT_ITEMS = WORKSPACE / "ryzomcore_leveldesign/game_element/sitem"
DEFAULT_CREATURES = WORKSPACE / "ryzomcore_leveldesign/game_elem/creature"
DEFAULT_DFN = WORKSPACE / "ryzomcore_leveldesign/DFN"
DEFAULT_FAMILIES_H = (WORKSPACE
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


def deep_merge(base: dict, over: dict) -> dict:
    """George PARENT semantics: child atoms override per-key, structs merge
    recursively, arrays replace wholesale."""
    out = dict(base)
    for k, v in over.items():
        if isinstance(v, dict) and isinstance(out.get(k), dict):
            out[k] = deep_merge(out[k], v)
        else:
            out[k] = v
    return out


class GeorgeCorpus:
    """All sheets of one extension under a root, with memoized PARENT-merged
    bodies. Parent refs are bare filenames, matched case-insensitively."""

    def __init__(self, root: Path, ext: str):
        self.by_name: dict[str, Path] = {}
        for f in sorted(root.rglob(f"*.{ext}")):
            key = f.name.lower()
            if key in self.by_name:
                # duplicate filename in another dir — first (sorted) wins
                continue
            self.by_name[key] = f
        self._cache: dict[str, dict | None] = {}

    def real_sheets(self):
        """Importable sheets: underscore-prefixed files are abstract templates
        absent from sheet_id.bin — parents only, never rows."""
        for key in sorted(self.by_name):
            f = self.by_name[key]
            if not f.stem.startswith("_"):
                yield f

    def merged(self, path: Path, warnings: list[str]) -> dict | None:
        key = path.name.lower()
        if key in self._cache:
            return self._cache[key]
        try:
            form = ET.parse(path).getroot()
        except ET.ParseError as e:
            warnings.append(f"{path}: XML parse error: {e}")
            self._cache[key] = None
            return None
        body = form.find("STRUCT")
        doc = struct_to_dict(body) if body is not None else {}
        merged: dict = {}
        for parent in form.findall("PARENT"):
            pname = parent.get("Filename", "").lower()
            ppath = self.by_name.get(pname)
            if ppath is None:
                warnings.append(f"{path}: parent {pname!r} not found")
                continue
            pdoc = self.merged(ppath, warnings)
            if pdoc:
                merged = deep_merge(merged, pdoc)
        merged = deep_merge(merged, doc)
        self._cache[key] = merged
        return merged


class TypEnum:
    """DFN .typ enum: DEFINITION Label ("raw material (mp)") -> Value
    ("RAW_MATERIAL"). Sheets store labels; SQL enum types use values."""

    def __init__(self, dfn_root: Path, filename: str):
        matches = list(dfn_root.rglob(filename))
        if not matches:
            raise SystemExit(f"{filename} not found under {dfn_root}")
        self.label_to_value: dict[str, str] = {}
        for d in ET.parse(matches[0]).getroot().iter("DEFINITION"):
            self.label_to_value[d.get("Label", "").lower()] = d.get("Value", "")
        self.values = set(self.label_to_value.values())

    def resolve(self, raw: str) -> str | None:
        if raw in self.values:
            return raw
        return self.label_to_value.get(raw.lower())


def george_bool(raw, default: bool) -> bool:
    if isinstance(raw, str) and raw.strip():
        return raw.strip().lower() in ("true", "1", "yes")
    return default


def george_num(raw, cast, default=0):
    try:
        return cast(float(raw))
    except (TypeError, ValueError):
        return default


# SQL enum membership (001_sheet_schema.sql). The C++ EItemFamily has a few
# extra members (AI, BRICK, DEAD_SEED, GUILD_FLAG) the schema deliberately
# excludes — values outside these sets fall back with a warning.
SQL_ITEM_FAMILIES = {
    "UNDEFINED", "ARMOR", "MELEE_WEAPON", "RANGE_WEAPON", "SHIELD", "AMMO",
    "CRAFTING_TOOL", "HARVEST_TOOL", "TAMING_TOOL", "TRAINING_TOOL", "CORPSE",
    "CARRION", "BAG", "STACK", "RAW_MATERIAL", "FOOD", "JEWELRY", "TELEPORT",
    "LIVING_SEED", "LITTLE_SEED", "MEDIUM_SEED", "BIG_SEED", "VERY_BIG_SEED",
    "MISSION_ITEM", "CRYSTALLIZED_SPELL", "ITEM_SAP_RECHARGE",
    "PET_ANIMAL_TICKET", "GUILD_OPTION", "HANDLED_ITEM", "COSMETIC", "SERVICE",
    "CONSUMABLE", "XP_CATALYSER", "SCROLL", "SCROLL_R2", "COMMAND_TICKET",
    "GENERIC_ITEM",
}
SQL_ITEM_ORIGINS = {"Common", "Fyros", "Matis", "Tryker", "Zorai", "Tribe",
                    "Karavan", "Refugee", "Kami"}

# basics keys promoted to real `items` columns; the rest stay in extras
ITEM_PROMOTED = {"name", "origin", "family", "ItemType", "stackable", "Quality",
                 "Bulk", "Weight", "Saleable", "Drop or Sell", "Price",
                 "Consumable", "CraftPlan"}


def parse_items(items_root: Path, dfn_root: Path, warnings: list[str]):
    fam_typ = TypEnum(dfn_root, "item_family.typ")
    type_typ = TypEnum(dfn_root, "item_type.typ")
    origin_typ = TypEnum(dfn_root, "item_origine.typ")
    corpus = GeorgeCorpus(items_root, "sitem")

    rows, stat_rows = [], []
    for f in corpus.real_sheets():
        doc = corpus.merged(f, warnings)
        if doc is None:
            continue
        basics = doc.get("basics", {})

        family = fam_typ.resolve(basics.get("family", "undefined")) or "UNDEFINED"
        if family not in SQL_ITEM_FAMILIES:
            warnings.append(f"{f}: family {basics.get('family')!r} -> {family} "
                            "not in SQL enum, using UNDEFINED")
            family = "UNDEFINED"
        item_type = type_typ.resolve(basics.get("ItemType", "undefined")) or None
        if item_type is None and basics.get("ItemType"):
            warnings.append(f"{f}: ItemType {basics['ItemType']!r} unresolved, "
                            "using UNDEFINED")
        origin = origin_typ.resolve(basics.get("origin", "common")) or None
        if origin not in SQL_ITEM_ORIGINS:
            if basics.get("origin"):
                warnings.append(f"{f}: origin {basics.get('origin')!r} unresolved, "
                                "using Common")
            origin = "Common"

        # extras: type-specific sub-DFN blocks + unpromoted basics keys
        extras = {k: v for k, v in doc.items() if k != "basics"}
        leftover = {k: v for k, v in basics.items() if k not in ITEM_PROMOTED}
        if leftover:
            extras["basics"] = leftover

        item_id = f.stem.lower()
        rows.append({
            "id": item_id,
            "name": basics.get("name") or None,
            "origin": origin,
            "family": family,
            "item_type": item_type or "UNDEFINED",
            "stackable": george_num(basics.get("stackable"), int, 1),
            "quality": george_num(basics.get("Quality"), int, 0),
            "bulk": george_num(basics.get("Bulk"), float, 0),
            "weight": george_num(basics.get("Weight"), float, 0),
            "saleable": george_bool(basics.get("Saleable"), True),
            "drop_or_sell": george_bool(basics.get("Drop or Sell"), True),
            "price": george_num(basics.get("Price"), float, 0),
            "consumable": george_bool(basics.get("Consumable"), False),
            "craft_plan": basics.get("CraftPlan") or None,
            "req_skill": None, "req_skill_min": 0,
            "req_skill2": None, "req_skill2_min": 0,
            "extras": extras,
        })

        # item_stats EAV: numeric leaves of type-specific blocks (live-balance
        # targets), dotted keys; "3d" is client visuals — skipped.
        for block, content in doc.items():
            if block in ("basics", "3d") or not isinstance(content, dict):
                continue
            stack = [(block, content)]
            while stack:
                prefix, d = stack.pop()
                for k, v in d.items():
                    if isinstance(v, dict):
                        stack.append((f"{prefix}.{k}", v))
                    elif isinstance(v, str):
                        try:
                            num = float(v)
                        except ValueError:
                            continue
                        stat_rows.append({"item_id": item_id,
                                          "stat_key": f"{prefix}.{k}",
                                          "base_value": num,
                                          "quality_factor": 0})
    return rows, stat_rows


# creature sub-DFN STRUCT -> JSONB column (full corpus coverage, verified)
CREATURE_JSONB = {
    "Basics": "basics", "3d data": "model_3d", "Properties": "properties",
    "Resists": "resists", "Protections": "protections", "Combat": "combat",
    "Harvest": "harvest", "Damage Shield": "damage_shield",
    "Collision": "collision", "animal_bag": "animal_bag",
}
CREATURE_SCALARS = {"creature_level", "category", "race_code", "group_id",
                    "group_assist", "group_attack", "action_cfg", "r2_npc"}
ACTION_CFG_ARRAYS = {"melee_cfg": "melee", "range_cfg": "range",
                     "nuke_cfg": "nuke", "heal_cfg": "heal"}


def parse_creatures(creatures_root: Path, warnings: list[str]):
    corpus = GeorgeCorpus(creatures_root, "creature")
    rows, attack_rows, action_rows = [], [], []
    for f in corpus.real_sheets():
        doc = corpus.merged(f, warnings)
        if doc is None:
            continue
        cid = f.stem.lower()

        jsonb = {col: doc.get(struct, {}) for struct, col in CREATURE_JSONB.items()}
        extras = {k: v for k, v in doc.items()
                  if k not in CREATURE_JSONB and k not in CREATURE_SCALARS
                  and k not in ACTION_CFG_ARRAYS and k != "special_comp"
                  and not re.fullmatch(r"attack_list[0-7]", k)}

        rows.append({
            "id": cid,
            "alias": None,
            "category": doc.get("category") or None,
            "race_code": doc.get("race_code") or None,
            "group_id": doc.get("group_id") or None,
            "group_assist": doc.get("group_assist") or None,
            "group_attack": doc.get("group_attack") or None,
            "creature_level": george_num(doc.get("creature_level"), float, None)
                              if doc.get("creature_level") else None,
            "r2_npc": george_bool(doc.get("r2_npc"), False),
            "action_cfg": doc.get("action_cfg") or "1111f",
            "action_on_death": None, "item_right": None, "item_right_stat": None,
            "item_left": None, "item_left_stat": None,
            **jsonb,
            "loot": {}, "action_phrases": {}, "shopkeeper": {},
            "text_ids": {}, "localisation": {},
            "special_comp": doc.get("special_comp", []),
            "alt_clothes": [],
            "extras": extras,
            # factory snapshot for the GM dashboard "Revert to factory stats"
            "default_stats": {"basics": jsonb["basics"],
                              "protections": jsonb["protections"],
                              "resists": jsonb["resists"]},
        })

        for slot in range(8):
            ref = doc.get(f"attack_list{slot}", "")
            if isinstance(ref, str) and ref.strip():
                attack_rows.append({"creature_id": cid, "slot": slot,
                                    "attack_list": ref.strip()})
        for key, action_type in ACTION_CFG_ARRAYS.items():
            arr = doc.get(key, [])
            if not isinstance(arr, list):
                continue
            ordinal = 0
            for entry in arr:
                if isinstance(entry, str) and entry.strip():
                    action_rows.append({"creature_id": cid,
                                        "action_type": action_type,
                                        "ordinal": ordinal,
                                        "actionlist": entry.strip()})
                    ordinal += 1
    return rows, attack_rows, action_rows


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
    if isinstance(v, bool):
        return "TRUE" if v else "FALSE"
    if isinstance(v, (int, float)):
        return str(v)
    if isinstance(v, (dict, list)):
        return "'" + json.dumps(v, separators=(",", ":")).replace("'", "''") + "'::jsonb"
    return "'" + str(v).replace("'", "''") + "'"


BRICK_COLS = ["id", "family", "brick_type", "skill_req", "skill_min",
              "sap_cost", "hp_cost", "sta_cost", "description", "extras"]
ITEM_COLS = ["id", "name", "origin", "family", "item_type", "stackable",
             "quality", "bulk", "weight", "saleable", "drop_or_sell", "price",
             "consumable", "craft_plan", "req_skill", "req_skill_min",
             "req_skill2", "req_skill2_min", "extras"]
ITEM_STAT_COLS = ["item_id", "stat_key", "base_value", "quality_factor"]
CREATURE_COLS = ["id", "alias", "category", "race_code", "group_id",
                 "group_assist", "group_attack", "creature_level", "r2_npc",
                 "action_cfg", "action_on_death", "item_right",
                 "item_right_stat", "item_left", "item_left_stat", "basics",
                 "model_3d", "loot", "harvest", "collision", "properties",
                 "action_phrases", "shopkeeper", "combat", "text_ids",
                 "protections", "resists", "damage_shield", "localisation",
                 "animal_bag", "special_comp", "alt_clothes", "extras",
                 "default_stats"]
ATTACK_COLS = ["creature_id", "slot", "attack_list"]
ACTION_CFG_COLS = ["creature_id", "action_type", "ordinal", "actionlist"]

def emit_insert(table: str, cols: list[str], rows: list[dict],
                conflict: str) -> str:
    def lit(r, c):
        s = sql_literal(r[c])
        if table == "items" and c == "origin" and s != "NULL":
            return s + "::item_origin_enum"
        if table == "items" and c == "family" and s != "NULL":
            return s + "::item_family_enum"
        if table == "items" and c == "item_type" and s != "NULL":
            return s + "::item_type_enum"
        return s

    return "\n".join([
        f"INSERT INTO {table} ({', '.join(cols)}) VALUES",
        ",\n".join("    (" + ", ".join(lit(r, c) for c in cols) + ")"
                   for r in rows),
        f"ON CONFLICT ({conflict}) DO NOTHING;",
    ])


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--sheets", type=Path, default=DEFAULT_SHEETS,
                    help="sbrick root")
    ap.add_argument("--items", type=Path, default=DEFAULT_ITEMS)
    ap.add_argument("--creatures", type=Path, default=DEFAULT_CREATURES)
    ap.add_argument("--dfn", type=Path, default=DEFAULT_DFN)
    ap.add_argument("--families-h", type=Path, default=DEFAULT_FAMILIES_H)
    ap.add_argument("--only", default="bricks,items,creatures",
                    help="comma list: bricks,items,creatures")
    ap.add_argument("--out-sql", type=Path, default=None)
    ap.add_argument("--db-url", default=None)
    args = ap.parse_args()
    targets = {t.strip() for t in args.only.split(",")}

    warnings: list[str] = []
    # (table, cols, rows, conflict-target) in FK-safe insert order
    batches: list[tuple[str, list[str], list[dict], str]] = []

    if "bricks" in targets:
        families = BrickFamilies(args.families_h)
        print(f"parsed {len(families.values)} TBrickFamily enum entries",
              file=sys.stderr)
        files = sorted(args.sheets.rglob("*.sbrick"))
        rows = [r for f in files
                if (r := parse_sbrick(f, args.sheets, warnings, families))]
        print(f"parsed {len(rows)}/{len(files)} .sbrick files", file=sys.stderr)
        batches.append(("bricks", BRICK_COLS, rows, "id"))

    if "items" in targets:
        item_rows, stat_rows = parse_items(args.items, args.dfn, warnings)
        print(f"parsed {len(item_rows)} items, {len(stat_rows)} item stats",
              file=sys.stderr)
        batches.append(("items", ITEM_COLS, item_rows, "id"))
        batches.append(("item_stats", ITEM_STAT_COLS, stat_rows,
                        "item_id, stat_key"))

    if "creatures" in targets:
        c_rows, a_rows, cfg_rows = parse_creatures(args.creatures, warnings)
        print(f"parsed {len(c_rows)} creatures, {len(a_rows)} attack lists, "
              f"{len(cfg_rows)} action configs", file=sys.stderr)
        batches.append(("creatures", CREATURE_COLS, c_rows, "id"))
        batches.append(("creature_attack_lists", ATTACK_COLS, a_rows,
                        "creature_id, slot"))
        batches.append(("creature_action_configs", ACTION_CFG_COLS, cfg_rows,
                        "creature_id, action_type, ordinal"))

    if warnings:
        Path("migration_warnings.log").write_text("\n".join(warnings) + "\n")
        print(f"{len(warnings)} warnings -> migration_warnings.log",
              file=sys.stderr)

    if args.db_url:
        import psycopg2  # deferred: only needed for direct insert
        conn = psycopg2.connect(args.db_url)
        with conn, conn.cursor() as cur:
            for table, cols, rows, conflict in batches:
                if not rows:
                    continue
                cur.executemany(
                    f"INSERT INTO {table} ({', '.join(cols)}) "
                    f"VALUES ({', '.join('%s' for _ in cols)}) "
                    f"ON CONFLICT ({conflict}) DO NOTHING",
                    [[json.dumps(r[c]) if isinstance(r[c], (dict, list))
                      else r[c] for c in cols] for r in rows],
                )
                print(f"inserted (or kept) {len(rows)} rows -> {table}",
                      file=sys.stderr)
        conn.close()
        return

    parts = ["-- GENERATED by tools/sheet_migrate/migrate_sheets.py"]
    for table, cols, rows, conflict in batches:
        if not rows:
            continue
        parts.append(f"-- {len(rows)} rows -> {table}")
        parts.append(emit_insert(table, cols, rows, conflict))
    sql = "\n".join(parts) + "\n"
    if args.out_sql:
        args.out_sql.write_text(sql)
        print(f"wrote {args.out_sql}", file=sys.stderr)
    else:
        print(sql)


if __name__ == "__main__":
    main()
