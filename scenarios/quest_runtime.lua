-- quest_runtime.lua — the engine that executes a compiled quest graph
-- (scenario-graph/v1, emitted by go-services quest-compiler). Phase 5.3.
--
-- A compiled scenario does:
--     local quest = require("quest_runtime")
--     local story = quest.new_storyline({ id=..., name=... })
--     story:add_quest({ id=..., objectives={...} })
--     story:set_start("...")
--     return story
--
-- The host (EGS Lua runtime / dynamic_scenario_service) then drives it:
--     story:begin()
--     story:fire({ on="talk", npc="merchant_dexton" })   -- as world events arrive
--     story:choose("side_dexton")                         -- when a choice resolves
--     story:state()                                       -- for the quest journal
--
-- Consequences (spawn, give_item, xp, ...) are appended to story.effects and,
-- if story.on_effect is set, dispatched to the host (which maps them to egs.*
-- calls / NATS). Pure Lua, no engine dependencies, so it is unit-testable.

local M = {}
M.__index = M

-- trigger_matches reports whether a world event satisfies an objective trigger.
local function trigger_matches(trig, ev)
  if trig.on ~= ev.on then return false end
  if trig.on == "talk" then
    return trig.npc == ev.npc
  elseif trig.on == "kill" then
    return trig.creature == ev.creature
  elseif trig.on == "collect" then
    return trig.item == ev.item
  elseif trig.on == "enter_zone" then
    return trig.zone == ev.zone
  elseif trig.on == "reach" then
    if ev.objective then return true end -- host may signal arrival by objective id
    if ev.x and ev.y and trig.x and trig.y then
      local dx, dy = ev.x - trig.x, ev.y - trig.y
      local r = trig.radius or 5
      return (dx * dx + dy * dy) <= (r * r)
    end
    return false
  elseif trig.on == "survive" then
    return true
  end
  return false
end

function M.new_storyline(def)
  local self = setmetatable({}, M)
  self.id = def.id
  self.name = def.name
  self.quests = {}        -- id -> quest table
  self.quest_order = {}   -- declaration order
  self.start_quest = nil
  -- runtime state
  self.active_quest = nil
  self.obj_index = 0      -- 1-based index into the active quest's objectives
  self.progress = 0       -- accumulated count for the current objective
  self.pending_choice = nil
  self.completed_quests = {}
  self.effects = {}       -- ordered log of fired consequences
  self.journal = {}       -- ordered list of completed { quest, objective }
  self.on_effect = nil    -- optional host hook(effect)
  return self
end

function M:add_quest(q)
  assert(q and q.id, "quest needs an id")
  self.quests[q.id] = q
  self.quest_order[#self.quest_order + 1] = q.id
  return self
end

function M:set_start(id) self.start_quest = id end

local function current_objective(self)
  if not self.active_quest then return nil end
  local q = self.quests[self.active_quest]
  return q and q.objectives[self.obj_index] or nil
end

function M:_arm_quest(id)
  assert(self.quests[id], "no such quest: " .. tostring(id))
  self.active_quest = id
  self.obj_index = 1
  self.progress = 0
  self.pending_choice = nil
end

-- begin activates the start quest and arms its first objective.
function M:begin()
  local start = self.start_quest or self.quest_order[1]
  self:_arm_quest(start)
  return self
end

function M:_fire_effects(obj)
  if not obj.consequences then return end
  for _, c in ipairs(obj.consequences) do
    self.effects[#self.effects + 1] = c
    if self.on_effect then self.on_effect(c) end
  end
end

-- fire feeds a world event to the active objective. Returns true if the event
-- advanced or progressed the active objective, false if it was irrelevant.
function M:fire(ev)
  if self.pending_choice then return false end
  local obj = current_objective(self)
  if not obj then return false end
  if not trigger_matches(obj.trigger, ev) then return false end

  local need = obj.trigger.count or 1
  self.progress = self.progress + (ev.count or 1)
  if self.progress < need then
    return true -- progressed, objective not yet complete
  end

  self:_fire_effects(obj)
  self.journal[#self.journal + 1] = { quest = self.active_quest, objective = obj.id }

  if obj.choice then
    self.pending_choice = { quest = self.active_quest, objective = obj.id, choice = obj.choice }
    return true
  end

  local q = self.quests[self.active_quest]
  if self.obj_index < #q.objectives then
    self.obj_index = self.obj_index + 1
    self.progress = 0
  else
    self.completed_quests[self.active_quest] = true
    self.active_quest = nil
  end
  return true
end

-- choose resolves a pending branch choice and routes to the option's next_quest.
-- Returns ok, chosen_option (or ok=false, error string).
function M:choose(option_id)
  if not self.pending_choice then return false, "no pending choice" end
  local chosen
  for _, opt in ipairs(self.pending_choice.choice.options) do
    if opt.id == option_id then chosen = opt break end
  end
  if not chosen then return false, "unknown option: " .. tostring(option_id) end

  self.completed_quests[self.active_quest] = true
  self.pending_choice = nil
  if chosen.next_quest and chosen.next_quest ~= "" then
    self:_arm_quest(chosen.next_quest)
  else
    self.active_quest = nil -- branch ends the storyline
  end
  return true, chosen
end

-- state is the snapshot the Godot quest journal renders.
function M:state()
  local obj = current_objective(self)
  return {
    storyline = self.id,
    active_quest = self.active_quest,
    objective = obj and obj.id or nil,
    objective_text = obj and obj.text or nil,
    awaiting_choice = self.pending_choice ~= nil,
    choice_prompt = self.pending_choice and self.pending_choice.choice.prompt or nil,
    choice_options = self.pending_choice and self.pending_choice.choice.options or nil,
  }
end

-- is_finished reports whether no quest is active (storyline complete).
function M:is_finished()
  return self.active_quest == nil and self.pending_choice == nil
end

return M
