# Scenario Graph Format (`scenario-graph/v1`)

The Quest Editor (Phase 5.3) saves a storyline as a JSON *scenario graph*. The
`quest-compiler` turns that graph into a Lua scenario script that runs on
`quest_runtime.lua` — the same embedded Lua used by the EGS (Task 4.4) and the
dynamic_scenario_service. The GM never writes Lua.

```
Quest Editor ──JSON──▶ quest-compiler ──Lua──▶ quest_runtime.lua ──▶ EGS / dynamic_scenario_service
 (node graph)        (this package)          (engine)               (live quest state + journal)
```

## The graph

A **storyline** has an id, a name, a `start_quest`, and an ordered list of
**quests**. A quest has ordered **objectives**. Each objective has a **trigger**
that advances it and optional **consequences** that fire on completion. The last
objective of a quest may carry a **choice** that branches into follow-up quests.

```jsonc
{
  "schema": "scenario-graph/v1",
  "id": "karavan_conspiracy",
  "name": "The Karavan Conspiracy",
  "start_quest": "missing_caravan",
  "quests": [
    {
      "id": "missing_caravan",
      "name": "The Missing Caravan",
      "start": { "on": "enter_zone", "zone": "fyros_start" },   // advisory auto-start
      "objectives": [
        { "id": "talk_dexton", "text": "Talk to Merchant Dexton",
          "trigger": { "on": "talk", "npc": "merchant_dexton" } },
        { "id": "find_wreck", "text": "Find the wreck",
          "trigger": { "on": "reach", "x": 1000, "y": 2000, "radius": 20 },
          "consequences": [ { "action": "spawn", "creature": "bandit", "count": 3 } ] },
        { "id": "return_letter", "text": "Return to Dexton",
          "trigger": { "on": "talk", "npc": "merchant_dexton" },
          "choice": {
            "prompt": "What do you tell Dexton?",
            "mode": "group_vote",
            "options": [
              { "id": "side_dexton", "text": "The truth", "next_quest": "side_dexton" },
              { "id": "side_bandits", "text": "Cover for them", "next_quest": "side_bandits" }
            ] } }
      ]
    }
    // ... side_dexton, side_bandits quests ...
  ]
}
```

### Triggers (`trigger.on`)

| `on` | Fields | Advances when |
|---|---|---|
| `talk` | `npc` | the player talks to `npc` |
| `kill` | `creature`, `count` | `count` of `creature` are killed |
| `collect` | `item`, `count` | `count` of `item` are collected |
| `reach` | `x`, `y`, `radius` | the player enters the radius (host may also signal by objective id) |
| `enter_zone` | `zone` | the player enters `zone` (used for quest auto-start) |
| `survive` | `seconds` | the host signals the timer elapsed |

### Consequences (`consequences[].action`)

`spawn` (`creature`,`count`) · `give_item` (`item`,`count`) · `xp` (`amount`) ·
`faction` (`faction`,`amount`) · `world_flag` (`flag`,`value`) · `message` (`text`).

They are appended to `story.effects` and, if the host sets `story.on_effect`,
dispatched live (the EGS maps them to `egs.*` calls / NATS).

### Choices

`mode` is one of `initiator | group_vote | lead | secret` (Phase 5.3a / the
Phase 5.8 vote system consume it). Each option's `next_quest` routes the branch;
an empty `next_quest` ends the storyline branch. Validation requires ≥ 2 options
and every `next_quest` to reference a defined quest.

## Compiling

```bash
# from go-services/
go run ./cmd/quest-compiler -in scenarios/examples/karavan_conspiracy.json -out story.lua
# or stream:  cat graph.json | go run ./cmd/quest-compiler > story.lua
```

The compiler validates the graph (unknown triggers/consequences, dangling
branches, duplicate ids, missing start quest all fail) before emitting Lua.

## Running / driving (host API)

```lua
local story = dofile("story.lua")   -- returns the storyline object
story:begin()                        -- arm the start quest
story:fire({ on = "talk", npc = "merchant_dexton" })  -- feed world events
story:choose("side_dexton")          -- resolve a pending branch choice
local s = story:state()              -- { active_quest, objective, awaiting_choice, ... } for the journal
story:is_finished()                  -- true when no quest is active
```

## Tests

- Compiler: `go test ./internal/questc/` (schema validation + emitted Lua).
- Runtime + worked example (needs lua5.2):

  ```bash
  go run ./cmd/quest-compiler -in scenarios/examples/karavan_conspiracy.json -out /tmp/k.lua
  lua5.2 scenarios/test_runtime.lua /tmp/k.lua    # 21 checks: full branching playthrough
  ```
