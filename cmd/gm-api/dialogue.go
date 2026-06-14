package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/boneysan/ryzom/go-services/internal/questc"
)

// generateDialogueReq is the GM's authoring-time request: describe the NPC and
// the situation, get back a branching dialogue node to drop into a quest.
type generateDialogueReq struct {
	NPC        string `json:"npc"`         // NPC name / sheet
	Persona    string `json:"persona"`     // persona, faction, role, tone
	Situation  string `json:"situation"`   // the quest beat / what's happening
	NumOptions int    `json:"num_options"` // how many player choices (default 3)
}

// llmDialogue is the exact JSON shape we ask the model to return.
type llmDialogue struct {
	NPCLine string `json:"npc_line"`
	Options []struct {
		Text     string `json:"text"`
		Response string `json:"response"`
	} `json:"options"`
}

// generateDialogue produces NPC dialogue AT AUTHORING TIME. The result is a
// scenario-graph choice (npc line + branching player options) the GM reviews and
// edits in the Quest Editor — it is not a runtime chatbot. next_quest routing is
// left empty for the GM to wire to follow-up quests.
func (s *server) generateDialogue(w http.ResponseWriter, r *http.Request) {
	var req generateDialogueReq
	if !decode(w, r, &req) {
		return
	}
	if req.Situation == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "situation is required")
		return
	}
	if req.NumOptions < 2 {
		req.NumOptions = 3
	}
	if req.NumOptions > 5 {
		req.NumOptions = 5
	}
	if s.geminiKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "service_unavailable", "GEMINI_API_KEY is not configured")
		return
	}

	systemPrompt := fmt.Sprintf(`You write NPC dialogue for the MMORPG Ryzom (the world of Atys). Stay in canon and in character; do not invent lore beyond what you are told.
Output ONLY valid JSON, no markdown, matching exactly this shape:
{"npc_line":"<1-3 sentence line the NPC speaks>","options":[{"text":"<player choice>","response":"<1-2 sentence NPC reply>"}]}
Produce exactly %d options. Each option is a distinct in-character player response that fits the situation; each "response" is how the NPC reacts. Keep everything concise.`, req.NumOptions)

	userPrompt := fmt.Sprintf("NPC: %s\nPersona/faction/tone: %s\nSituation: %s", req.NPC, req.Persona, req.Situation)

	gReq := buildGeminiRequest(userPrompt, systemPrompt)
	body, _ := json.Marshal(gReq)

	resp, err := geminiHTTP.Post(geminiURL(s.geminiModel, s.geminiKey), "application/json", bytes.NewReader(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		writeErr(w, http.StatusInternalServerError, "ai_error", fmt.Sprintf("Gemini API returned status %d: %s", resp.StatusCode, string(errBody)))
		return
	}

	var gResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", "Failed to parse Gemini response")
		return
	}
	if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
		writeErr(w, http.StatusInternalServerError, "ai_error", "Gemini returned empty response")
		return
	}

	choice, err := dialogueToChoice(req.NPC, gResp.Candidates[0].Content.Parts[0].Text)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, choice)
}

// dialogueToChoice parses the model's JSON output into a scenario-graph Choice.
// Split out so it can be unit-tested without a live model.
func dialogueToChoice(npc, raw string) (questc.Choice, error) {
	var dlg llmDialogue
	if err := json.Unmarshal([]byte(stripJSONFences(raw)), &dlg); err != nil {
		return questc.Choice{}, fmt.Errorf("LLM output invalid JSON: %w", err)
	}
	if dlg.NPCLine == "" || len(dlg.Options) < 2 {
		return questc.Choice{}, fmt.Errorf("LLM output missing npc_line or needs >=2 options")
	}

	choice := questc.Choice{
		Prompt:  dlg.NPCLine,
		Mode:    "initiator",
		NPC:     npc,
		NPCLine: dlg.NPCLine,
	}
	for i, o := range dlg.Options {
		choice.Options = append(choice.Options, questc.ChoiceOption{
			ID:       fmt.Sprintf("opt_%d", i+1),
			Text:     o.Text,
			Response: o.Response,
		})
	}
	return choice, nil
}
