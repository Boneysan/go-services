-- test_runtime.lua — runs a compiled scenario against quest_runtime and asserts
-- the full three-objective branching playthrough. Phase 5.3 acceptance check.
--   usage: lua5.2 test_runtime.lua /path/to/compiled.lua
--
-- Proves: the compiled Lua loads without error; firing each objective's trigger
-- advances the quest; count triggers need N events; consequences fire; the final
-- objective's choice branches into the chosen follow-up quest.

local here = (arg[0] or ""):match("(.*/)") or "./"
package.path = here .. "?.lua;" .. package.path

local compiled = arg[1] or error("pass the compiled scenario lua path as arg 1")
local story = dofile(compiled)

local fails = 0
local function check(cond, msg)
  if cond then
    io.write("  ok   - " .. msg .. "\n")
  else
    fails = fails + 1
    io.write("  FAIL - " .. msg .. "\n")
  end
end
local function effect_fired(pred)
  for _, e in ipairs(story.effects) do if pred(e) then return true end end
  return false
end

story:begin()
check(story:state().active_quest == "missing_caravan", "start quest is missing_caravan")
check(story:state().objective == "talk_dexton", "first objective armed (talk_dexton)")

check(story:fire({ on = "talk", npc = "merchant_dexton" }), "talk to Dexton advances")
check(story:state().objective == "find_wreck", "advanced to find_wreck")

check(story:fire({ on = "talk", npc = "random_guy" }) == false, "irrelevant event is ignored")

check(story:fire({ on = "reach", x = 1000, y = 2000 }), "reaching the wreck advances")
check(effect_fired(function(e) return e.action == "spawn" and e.creature == "bandit" end), "spawn-bandit consequence fired")
check(effect_fired(function(e) return e.action == "message" end), "ambush message consequence fired")
check(story:state().objective == "defeat_bandits", "advanced to defeat_bandits")

story:fire({ on = "kill", creature = "bandit" })
story:fire({ on = "kill", creature = "bandit" })
check(story:state().objective == "defeat_bandits", "still on defeat_bandits after 2/3 kills")
story:fire({ on = "kill", creature = "bandit" })
check(story:state().objective == "return_letter", "advanced to return_letter after 3/3 kills")
check(effect_fired(function(e) return e.action == "give_item" and e.item == "bloodstained_letter" end), "give_item letter fired")

check(story:fire({ on = "talk", npc = "merchant_dexton" }), "returning to Dexton triggers the choice")
check(story:state().awaiting_choice, "awaiting branch choice")
check(story:state().choice_prompt == "What do you tell Dexton?", "choice prompt exposed for the journal")

local ok, chosen = story:choose("side_bandits")
check(ok and chosen.next_quest == "side_bandits", "chose the side_bandits branch")
check(story:state().active_quest == "side_bandits", "branched into side_bandits quest")
check(story:state().objective == "meet_bandit_chief", "side_bandits first objective armed")

check(story:choose("anything") == false, "choosing again (no pending choice) returns false")

story:fire({ on = "reach", x = 1500, y = 2400 })
check(story:is_finished(), "storyline finished after the final branch objective")
check(effect_fired(function(e) return e.action == "faction" and e.amount == -800 end), "negative Fyros standing fired on the bandit path")

if fails > 0 then
  io.write(("FAILED: %d check(s)\n"):format(fails))
  os.exit(1)
else
  io.write("ALL PASS\n")
  os.exit(0)
end
