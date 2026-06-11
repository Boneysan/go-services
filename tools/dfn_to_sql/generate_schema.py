#!/usr/bin/env python3
"""DFN -> PostgreSQL schema generator (Phase 4, Task 4.1a).

Parses the Georges sheet schema definitions from ryzomcore_leveldesign:
  - *.typ  (231 files): scalar type + enum definitions
  - *.dfn  (422 files): sheet structure definitions

and emits CREATE TYPE / CREATE TABLE SQL for selected root sheet types.

Format facts (verified against the actual checkout, 2026-06-11 — these
correct two claims in Code_Investigation_Checklist.md Task 5):
  - 27 DFN files use <PARENT Name="..."/> inheritance in addition to
    composition; parent ELEMENTs are merged in before the file's own.
  - ELEMENT Type is one of "Type", "Dfn", or "DfnPointer" (21 uses).
    DfnPointer is a by-reference link to another sheet -> TEXT column.

Mapping rules (Phase_4_Data_Driven_Mechanics.md, Task 4.1a):
  - Type="Type"  -> column. SQL type from the referenced .typ file.
  - Type="Dfn"   -> JSONB column named after the element (sub-DFN block).
  - Array="true" -> junction table (parent_id, ordinal, value).
  - .typ with <DEFINITION> entries and base String -> CREATE TYPE ... AS ENUM.
  - FilenameExt="*.xxx" -> cross-sheet reference; emitted as a column
    comment, NOT a foreign key (referenced sheet tables may not exist yet).

The raw output is NOT applied directly; it is reviewed and hand-tuned into
migrations/001_sheet_schema.sql (see that file's header for the deltas).

Usage:
  generate_schema.py [--leveldesign PATH] [--roots item.dfn creature.dfn ...]
                     [--list-roots] [--out FILE]

Stdlib only (xml.etree handles all 653 files cleanly; lxml not required).
"""

import argparse
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

DEFAULT_LEVELDESIGN = Path(__file__).resolve().parents[3] / "ryzomcore_leveldesign"
DEFAULT_ROOTS = ["item.dfn", "creature.dfn", "sbrick.dfn", "loot_table.dfn"]

# Georges base types -> PostgreSQL types
BASE_TYPE_SQL = {
    "SignedInt": "BIGINT",
    "UnsignedInt": "BIGINT",
    "Double": "DOUBLE PRECISION",
    "String": "TEXT",
    "Color": "TEXT",  # serialized "R G B A"
}


def sanitize(name: str) -> str:
    """'Drop or Sell' -> drop_or_sell; 'Type 1' -> type_1; '3d data' -> col_3d_data."""
    s = re.sub(r"[^A-Za-z0-9]+", "_", name.strip()).strip("_").lower()
    s = re.sub(r"_+", "_", s)
    if not s:
        s = "unnamed"
    if s[0].isdigit():
        s = "col_" + s
    return s


class TypDef:
    def __init__(self, path: Path):
        self.path = path
        self.name = path.stem.lower()
        root = ET.parse(path).getroot()
        self.base = root.get("Type", "String")
        self.default = root.get("Default")
        self.min = root.get("Min")
        self.max = root.get("Max")
        # Preserve definition order; drop duplicate values (e.g. TELEPORT
        # appears twice in item_family.typ).
        seen, vals = set(), []
        for d in root.findall("DEFINITION"):
            v = d.get("Value")
            if v is not None and v not in seen:
                seen.add(v)
                vals.append(v)
        self.enum_values = vals

    @property
    def is_boolean(self) -> bool:
        return self.base == "String" and {v.lower() for v in self.enum_values} == {"true", "false"}

    @property
    def is_enum(self) -> bool:
        return self.base == "String" and len(self.enum_values) >= 2 and not self.is_boolean

    @property
    def enum_sql_name(self) -> str:
        return sanitize(self.name) + "_enum"

    def sql_type(self) -> str:
        if self.is_boolean:
            return "BOOLEAN"
        if self.is_enum:
            return self.enum_sql_name
        if self.base in ("SignedInt", "UnsignedInt"):
            # Narrow to INTEGER when the declared range fits in 32 bits.
            try:
                if (self.min is not None and self.max is not None
                        and -2**31 <= int(self.min) and int(self.max) <= 2**31 - 1):
                    return "INTEGER"
            except ValueError:
                pass
            return BASE_TYPE_SQL[self.base]
        return BASE_TYPE_SQL.get(self.base, "TEXT")


class DfnElement:
    def __init__(self, el: ET.Element):
        self.name = el.get("Name", "")
        self.kind = el.get("Type", "Type")  # Type | Dfn | DfnPointer
        self.filename = (el.get("Filename") or "").lower()
        self.array = el.get("Array", "").lower() == "true"
        self.default = el.get("Default")
        self.filename_ext = el.get("FilenameExt")  # e.g. "*.sitem"


class DfnDef:
    def __init__(self, path: Path):
        self.path = path
        self.name = path.stem.lower()
        root = ET.parse(path).getroot()
        self.parents = [(p.get("Name") or "").lower() for p in root.findall("PARENT")]
        self.elements = [DfnElement(e) for e in root.findall("ELEMENT")]


class SchemaGenerator:
    def __init__(self, leveldesign: Path):
        self.typs: dict[str, TypDef] = {}
        self.dfns: dict[str, DfnDef] = {}
        self.warnings: list[str] = []
        for p in sorted(leveldesign.rglob("*.typ")):
            t = TypDef(p)
            self.typs[p.name.lower()] = t
        for p in sorted(leveldesign.rglob("*.dfn")):
            d = DfnDef(p)
            # Later duplicates of the same basename shadow earlier ones;
            # the checkout has none that conflict, but warn if they do.
            if p.name.lower() in self.dfns:
                self.warnings.append(f"duplicate dfn basename: {p}")
            self.dfns[p.name.lower()] = d
        self.used_enums: dict[str, TypDef] = {}

    def merged_elements(self, dfn: DfnDef, _seen=None) -> list[DfnElement]:
        """Elements with PARENT inheritance merged in (parents first)."""
        _seen = _seen or set()
        if dfn.name in _seen:
            self.warnings.append(f"parent cycle at {dfn.name}")
            return []
        _seen.add(dfn.name)
        out: list[DfnElement] = []
        for pname in dfn.parents:
            parent = self.dfns.get(pname)
            if parent is None:
                self.warnings.append(f"{dfn.path.name}: missing parent {pname}")
                continue
            out.extend(self.merged_elements(parent, _seen))
        out.extend(dfn.elements)
        # Child redefinitions override parent fields of the same name.
        by_name: dict[str, DfnElement] = {}
        for e in out:
            by_name[sanitize(e.name)] = e
        return list(by_name.values())

    def table_for(self, root_dfn_name: str) -> str:
        dfn = self.dfns.get(root_dfn_name.lower())
        if dfn is None:
            raise SystemExit(f"root DFN not found: {root_dfn_name}")
        table = sanitize(dfn.path.stem) + "s"
        cols: list[tuple[str, str]] = [("id TEXT PRIMARY KEY", "sheet filename without extension")]
        junctions: list[str] = []

        for el in self.merged_elements(dfn):
            col = sanitize(el.name)
            if el.kind == "Dfn":
                if el.array:
                    # Array of sub-DFN blocks: junction table of JSONB rows.
                    junctions.append(self._junction(table, col, "JSONB"))
                else:
                    cols.append((f"{col} JSONB DEFAULT '{{}}'", f"sub-DFN {el.filename}"))
                continue
            if el.kind == "DfnPointer":
                cols.append((f"{col} TEXT", f"DfnPointer -> {el.filename}"))
                continue
            # Type="Type"
            typ = self.typs.get(el.filename)
            if typ is None:
                self.warnings.append(f"{dfn.path.name}: unknown typ {el.filename!r} for {el.name!r}")
                sql_t = "TEXT"
            else:
                sql_t = typ.sql_type()
                if typ.is_enum:
                    self.used_enums[typ.enum_sql_name] = typ
            if el.array:
                junctions.append(self._junction(table, col, sql_t))
                continue
            line = f"{col} {sql_t}"
            dflt = self._sql_default(el, typ, sql_t)
            if dflt is not None:
                line += f" DEFAULT {dflt}"
            cols.append((line, f"sheet ref {el.filename_ext}" if el.filename_ext else ""))

        rendered = []
        for i, (defn, comment) in enumerate(cols):
            comma = "," if i < len(cols) - 1 else ""
            rendered.append(f"    {defn}{comma}" + (f"  -- {comment}" if comment else ""))
        ddl = f"CREATE TABLE IF NOT EXISTS {table} (\n" + "\n".join(rendered) + "\n);"
        return "\n\n".join([ddl] + junctions)

    @staticmethod
    def _junction(table: str, col: str, sql_t: str) -> str:
        jt = f"{table}_{col}"
        return (
            f"CREATE TABLE IF NOT EXISTS {jt} (\n"
            f"    {table[:-1]}_id TEXT NOT NULL REFERENCES {table}(id) ON DELETE CASCADE,\n"
            f"    ordinal INTEGER NOT NULL,\n"
            f"    value {sql_t},\n"
            f"    PRIMARY KEY ({table[:-1]}_id, ordinal)\n"
            f");"
        )

    def _sql_default(self, el: DfnElement, typ, sql_t: str):
        d = el.default
        if d is None or d == "":
            return None
        # Georges allows expression defaults like "Basics.Level" — skip those.
        if d.startswith('"'):
            return None
        if sql_t in ("INTEGER", "BIGINT", "DOUBLE PRECISION", "REAL"):
            try:
                float(d)
                return d
            except ValueError:
                return None
        if sql_t == "BOOLEAN":
            return d.lower() if d.lower() in ("true", "false") else None
        if typ is not None and typ.is_enum:
            return f"'{d}'" if d in typ.enum_values else None
        return "'" + d.replace("'", "''") + "'"

    def enums_sql(self) -> str:
        """Idempotent CREATE TYPE for every enum referenced by emitted tables."""
        out = []
        for name in sorted(self.used_enums):
            typ = self.used_enums[name]
            vals = ", ".join("'" + v.replace("'", "''") + "'" for v in typ.enum_values)
            out.append(
                "DO $$ BEGIN\n"
                f"    CREATE TYPE {name} AS ENUM ({vals});\n"
                "EXCEPTION WHEN duplicate_object THEN NULL;\n"
                "END $$;"
            )
        return "\n\n".join(out)

    def root_candidates(self) -> list[str]:
        """DFNs that look like top-level sheets (not _sub-DFN building blocks)."""
        return sorted(n for n in self.dfns if not n.startswith("_"))


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--leveldesign", type=Path, default=DEFAULT_LEVELDESIGN)
    ap.add_argument("--roots", nargs="+", default=DEFAULT_ROOTS,
                    help="root DFN basenames to emit tables for")
    ap.add_argument("--list-roots", action="store_true",
                    help="list all non-underscore DFNs and exit")
    ap.add_argument("--out", type=Path, default=None, help="write SQL here instead of stdout")
    args = ap.parse_args()

    if not args.leveldesign.is_dir():
        raise SystemExit(f"leveldesign checkout not found: {args.leveldesign}")
    gen = SchemaGenerator(args.leveldesign)
    print(f"-- parsed {len(gen.dfns)} DFN + {len(gen.typs)} TYP files", file=sys.stderr)

    if args.list_roots:
        print("\n".join(gen.root_candidates()))
        return

    tables = [gen.table_for(r) for r in args.roots]
    parts = [
        "-- GENERATED by tools/dfn_to_sql/generate_schema.py — review before applying.",
        f"-- roots: {', '.join(args.roots)}",
        gen.enums_sql(),
        *tables,
    ]
    sql = "\n\n".join(p for p in parts if p) + "\n"
    if args.out:
        args.out.write_text(sql)
        print(f"-- wrote {args.out}", file=sys.stderr)
    else:
        print(sql)

    for w in gen.warnings:
        print(f"-- WARNING: {w}", file=sys.stderr)


if __name__ == "__main__":
    main()
