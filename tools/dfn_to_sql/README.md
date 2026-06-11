# dfn_to_sql — DFN → PostgreSQL schema generator (Phase 4, Task 4.1a)

Parses the Georges sheet definitions in `ryzomcore_leveldesign` (422 `.dfn`
+ 231 `.typ` files) and emits `CREATE TYPE` / `CREATE TABLE` SQL.

```bash
# default roots: item.dfn creature.dfn sbrick.dfn loot_table.dfn
python3 generate_schema.py --out /tmp/schema.sql

# any sheet type can be a root
python3 generate_schema.py --list-roots
python3 generate_schema.py --roots outpost.dfn mission.dfn
```

Stdlib only — all 653 files parse with `xml.etree`; lxml (suggested in the
plan) is not needed.

**The raw output is not applied directly.** It is reviewed and hand-tuned
into `migrations/001_sheet_schema.sql`; the header of that file lists the
tuning deltas (promoted item core columns, attack-list/action-config
junction tables, `default_stats`, plus the Task 4.1b `bricks` and
Task 4.2c `characters` tables).

Format facts verified against the checkout (these correct
`Code_Investigation_Checklist.md` Task 5):

- 27 DFN files use `<PARENT>` inheritance in addition to composition;
  the generator merges parent elements (child fields override).
- ELEMENT `Type` is one of `Type`, `Dfn`, or `DfnPointer` (21 uses);
  `DfnPointer` maps to a TEXT reference column.
- `boolean.typ` is a String enum of true/false → mapped to `BOOLEAN`.
- Enum `.typ` files can contain duplicate values (e.g. TELEPORT twice in
  `item_family.typ`) — deduped, order preserved.
- Actual `.sbrick` data files (625) ARE present in the leveldesign checkout
  under `game_element/sbrick/` — only `.item`/`.creature` data requires the
  full game install.
