// Package questc compiles a GM-authored quest graph (the Quest Editor's
// scenario-graph/v1 JSON) into a Lua scenario script that runs on the same Lua
// runtime embedded in the EGS (Task 4.4) / dynamic_scenario_service. Phase 5.3.
//
// The graph is a storyline -> quests -> ordered objectives. Each objective has a
// trigger that advances it and optional consequences that fire on completion.
// The final objective of a quest may carry a branching choice whose options
// route to follow-up quests.
package questc

import "fmt"

// SchemaVersion is the only graph schema this compiler accepts.
const SchemaVersion = "scenario-graph/v1"

// allowedTriggers / allowedConsequences gate what the runtime understands.
var allowedTriggers = map[string]bool{
	"talk": true, "kill": true, "reach": true, "collect": true,
	"enter_zone": true, "survive": true,
}
var allowedConsequences = map[string]bool{
	"spawn": true, "give_item": true, "xp": true, "faction": true,
	"world_flag": true, "message": true,
}
var allowedModes = map[string]bool{
	"initiator": true, "group_vote": true, "lead": true, "secret": true,
}

// Storyline is the root of a scenario graph.
type Storyline struct {
	Schema     string  `json:"schema"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	StartQuest string  `json:"start_quest"`
	Quests     []Quest `json:"quests"`
}

type Quest struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Start      *Trigger    `json:"start,omitempty"` // advisory auto-start condition
	Objectives []Objective `json:"objectives"`
}

type Objective struct {
	ID           string        `json:"id"`
	Text         string        `json:"text"`
	Trigger      Trigger       `json:"trigger"`
	Consequences []Consequence `json:"consequences,omitempty"`
	Choice       *Choice       `json:"choice,omitempty"`
}

type Trigger struct {
	On       string  `json:"on"` // talk|kill|reach|collect|enter_zone|survive
	NPC      string  `json:"npc,omitempty"`
	Creature string  `json:"creature,omitempty"`
	Item     string  `json:"item,omitempty"`
	Zone     string  `json:"zone,omitempty"`
	Count    int     `json:"count,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Radius   float64 `json:"radius,omitempty"`
	Seconds  int     `json:"seconds,omitempty"`
}

type Consequence struct {
	Action   string `json:"action"` // spawn|give_item|xp|faction|world_flag|message
	Creature string `json:"creature,omitempty"`
	Item     string `json:"item,omitempty"`
	Count    int    `json:"count,omitempty"`
	Amount   int    `json:"amount,omitempty"`
	Faction  string `json:"faction,omitempty"`
	Flag     string `json:"flag,omitempty"`
	Value    string `json:"value,omitempty"`
	Text     string `json:"text,omitempty"`
}

type Choice struct {
	Prompt  string         `json:"prompt"`
	Mode    string         `json:"mode"` // initiator|group_vote|lead|secret
	Options []ChoiceOption `json:"options"`
}

type ChoiceOption struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	NextQuest string `json:"next_quest,omitempty"` // "" = ends the storyline branch
}

// Validate checks structural integrity and cross-references so the compiled Lua
// can't reference a quest or use a trigger the runtime doesn't know.
func (s *Storyline) Validate() error {
	if s.Schema != SchemaVersion {
		return fmt.Errorf("unsupported schema %q (want %q)", s.Schema, SchemaVersion)
	}
	if s.ID == "" {
		return fmt.Errorf("storyline id is required")
	}
	if len(s.Quests) == 0 {
		return fmt.Errorf("storyline %q has no quests", s.ID)
	}

	quests := make(map[string]bool, len(s.Quests))
	for _, q := range s.Quests {
		if q.ID == "" {
			return fmt.Errorf("a quest is missing its id")
		}
		if quests[q.ID] {
			return fmt.Errorf("duplicate quest id %q", q.ID)
		}
		quests[q.ID] = true
	}

	start := s.StartQuest
	if start == "" {
		start = s.Quests[0].ID
	}
	if !quests[start] {
		return fmt.Errorf("start_quest %q is not a defined quest", start)
	}

	for _, q := range s.Quests {
		if len(q.Objectives) == 0 {
			return fmt.Errorf("quest %q has no objectives", q.ID)
		}
		objs := make(map[string]bool, len(q.Objectives))
		for _, o := range q.Objectives {
			if o.ID == "" {
				return fmt.Errorf("quest %q has an objective with no id", q.ID)
			}
			if objs[o.ID] {
				return fmt.Errorf("quest %q has duplicate objective id %q", q.ID, o.ID)
			}
			objs[o.ID] = true

			if !allowedTriggers[o.Trigger.On] {
				return fmt.Errorf("quest %q objective %q: unknown trigger %q", q.ID, o.ID, o.Trigger.On)
			}
			if (o.Trigger.On == "kill" || o.Trigger.On == "collect") && o.Trigger.Count < 0 {
				return fmt.Errorf("quest %q objective %q: count must be >= 0", q.ID, o.ID)
			}
			for _, c := range o.Consequences {
				if !allowedConsequences[c.Action] {
					return fmt.Errorf("quest %q objective %q: unknown consequence %q", q.ID, o.ID, c.Action)
				}
			}
			if o.Choice != nil {
				if err := validateChoice(q.ID, o.ID, o.Choice, quests); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateChoice(qID, oID string, c *Choice, quests map[string]bool) error {
	if c.Mode != "" && !allowedModes[c.Mode] {
		return fmt.Errorf("quest %q objective %q: unknown choice mode %q", qID, oID, c.Mode)
	}
	if len(c.Options) < 2 {
		return fmt.Errorf("quest %q objective %q: a choice needs at least 2 options", qID, oID)
	}
	seen := make(map[string]bool, len(c.Options))
	for _, opt := range c.Options {
		if opt.ID == "" {
			return fmt.Errorf("quest %q objective %q: a choice option is missing its id", qID, oID)
		}
		if seen[opt.ID] {
			return fmt.Errorf("quest %q objective %q: duplicate choice option id %q", qID, oID, opt.ID)
		}
		seen[opt.ID] = true
		if opt.NextQuest != "" && !quests[opt.NextQuest] {
			return fmt.Errorf("quest %q objective %q option %q: next_quest %q is not a defined quest",
				qID, oID, opt.ID, opt.NextQuest)
		}
	}
	return nil
}
