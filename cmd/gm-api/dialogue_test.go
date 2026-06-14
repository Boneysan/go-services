package main

import (
	"strings"
	"testing"
)

func TestDialogueToChoice(t *testing.T) {
	// Model output wrapped in markdown fences, with two options.
	raw := "```json\n" + `{"npc_line":"Halt, outlander. The Fyros do not suffer spies.",
		"options":[
			{"text":"I'm no spy — I seek the elder.","response":"Hmph. The elder is past the gate. Mind your tongue."},
			{"text":"Stand aside, ash-eater.","response":"Bold words. Draw steel and we'll see."}
		]}` + "\n```"

	c, err := dialogueToChoice("fyros_guard", raw)
	if err != nil {
		t.Fatalf("dialogueToChoice error: %v", err)
	}
	if c.NPC != "fyros_guard" {
		t.Errorf("npc = %q, want fyros_guard", c.NPC)
	}
	if !strings.HasPrefix(c.NPCLine, "Halt") {
		t.Errorf("npc_line not carried through: %q", c.NPCLine)
	}
	if len(c.Options) != 2 {
		t.Fatalf("got %d options, want 2", len(c.Options))
	}
	if c.Options[0].ID != "opt_1" || c.Options[1].ID != "opt_2" {
		t.Errorf("option ids = %q,%q want opt_1,opt_2", c.Options[0].ID, c.Options[1].ID)
	}
	if c.Options[0].Response == "" {
		t.Error("option response not carried through")
	}

	// The generated choice must satisfy the schema validator when wired into a quest.
	if c.Mode != "initiator" {
		t.Errorf("mode = %q, want initiator", c.Mode)
	}
}

func TestDialogueToChoiceRejectsBad(t *testing.T) {
	for _, raw := range []string{
		`not json`,
		`{"npc_line":"hi","options":[{"text":"only one"}]}`, // <2 options
		`{"npc_line":"","options":[{"text":"a"},{"text":"b"}]}`, // no npc line
	} {
		if _, err := dialogueToChoice("npc", raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}
