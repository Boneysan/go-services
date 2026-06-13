package questc

import (
	"strings"
	"testing"
)

func sampleStoryline() *Storyline {
	return &Storyline{
		Schema:     SchemaVersion,
		ID:         "test_story",
		Name:       "Test Story",
		StartQuest: "q1",
		Quests: []Quest{
			{
				ID:   "q1",
				Name: "First Quest",
				Objectives: []Objective{
					{ID: "o1", Text: "Talk to NPC", Trigger: Trigger{On: "talk", NPC: "dexton"}},
					{
						ID: "o2", Text: "Kill 3 bandits",
						Trigger:      Trigger{On: "kill", Creature: "bandit", Count: 3},
						Consequences: []Consequence{{Action: "give_item", Item: "letter", Count: 1}},
					},
					{
						ID: "o3", Text: "Decide",
						Trigger: Trigger{On: "talk", NPC: "dexton"},
						Choice: &Choice{
							Prompt: "What now?", Mode: "group_vote",
							Options: []ChoiceOption{
								{ID: "a", Text: "Truth", NextQuest: "q2"},
								{ID: "b", Text: "Lie"}, // ends the branch
							},
						},
					},
				},
			},
			{ID: "q2", Name: "Second Quest", Objectives: []Objective{
				{ID: "x", Text: "Done", Trigger: Trigger{On: "talk", NPC: "dexton"}},
			}},
		},
	}
}

func TestCompileEmitsExpectedLua(t *testing.T) {
	out, err := Compile(sampleStoryline())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, want := range []string{
		`local quest = require("quest_runtime")`,
		`quest.new_storyline({ id = "test_story", name = "Test Story" })`,
		`story:add_quest({`,
		`id = "o2"`,
		`trigger = { on = "kill", creature = "bandit", count = 3 }`,
		`consequences = {`,
		`{ action = "give_item", item = "letter", count = 1 }`,
		`choice = {`,
		`mode = "group_vote"`,
		`{ id = "a", text = "Truth", next_quest = "q2" }`,
		`{ id = "b", text = "Lie" }`, // no next_quest emitted
		`story:set_start("q1")`,
		`return story`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("compiled Lua missing:\n  %s\n--- full output ---\n%s", want, out)
		}
	}
}

func TestValidateRejectsBadGraphs(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Storyline)
		substr string
	}{
		{"bad schema", func(s *Storyline) { s.Schema = "x/v9" }, "unsupported schema"},
		{"no quests", func(s *Storyline) { s.Quests = nil }, "no quests"},
		{"bad start", func(s *Storyline) { s.StartQuest = "nope" }, "start_quest"},
		{"dup quest", func(s *Storyline) { s.Quests = append(s.Quests, s.Quests[0]) }, "duplicate quest id"},
		{"unknown trigger", func(s *Storyline) { s.Quests[0].Objectives[0].Trigger.On = "sneeze" }, "unknown trigger"},
		{"dangling branch", func(s *Storyline) { s.Quests[0].Objectives[2].Choice.Options[0].NextQuest = "ghost" }, "not a defined quest"},
		{"one-option choice", func(s *Storyline) {
			s.Quests[0].Objectives[2].Choice.Options = s.Quests[0].Objectives[2].Choice.Options[:1]
		}, "at least 2 options"},
		{"unknown consequence", func(s *Storyline) { s.Quests[0].Objectives[1].Consequences[0].Action = "explode" }, "unknown consequence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sampleStoryline()
			tc.mutate(s)
			err := s.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.substr)
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.substr)
			}
		})
	}
}

func TestCompileValidStorylineSucceeds(t *testing.T) {
	if _, err := Compile(sampleStoryline()); err != nil {
		t.Fatalf("valid storyline failed to compile: %v", err)
	}
}

func TestLuaStringEscaping(t *testing.T) {
	got := luaStr(`he said "hi"` + "\n\t" + `c:\path`)
	want := `"he said \"hi\"\n\tc:\\path"`
	if got != want {
		t.Fatalf("luaStr = %s, want %s", got, want)
	}
}
