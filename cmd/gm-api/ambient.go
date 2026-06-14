package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/boneysan/ryzom/go-services/internal/ambientc"
)

// generateAmbientReq asks for a pool of ambient barks for a background NPC type.
type generateAmbientReq struct {
	Archetype string `json:"archetype"` // shopkeeper, guard, townsfolk, ...
	Faction   string `json:"faction"`   // fyros, matis, tryker, zorai, ...
	Region    string `json:"region"`    // pyr, yrkanis, ... (optional)
	Count     int    `json:"count"`     // how many lines (default 6)
	Notes     string `json:"notes"`     // freeform flavor hints (optional)
}

type ambientResult struct {
	Key   string   `json:"key"`
	Lines []string `json:"lines"`
}

// generateAmbient produces a pool of ambient flavor lines AT PREP TIME. These are
// pre-generated and stored in the ambient-dialogue library; nothing calls an LLM
// at play time. No branching, no quest hooks — pure background color.
func (s *server) generateAmbient(w http.ResponseWriter, r *http.Request) {
	var req generateAmbientReq
	if !decode(w, r, &req) {
		return
	}
	if req.Archetype == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "archetype is required")
		return
	}
	if req.Count < 1 {
		req.Count = 6
	}
	if req.Count > 20 {
		req.Count = 20
	}
	if s.geminiKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "service_unavailable", "GEMINI_API_KEY is not configured")
		return
	}

	systemPrompt := fmt.Sprintf(`You write short ambient NPC barks for the MMORPG Ryzom (the world of Atys). Stay in canon and in character. These are idle/flavor lines for a GENERIC background NPC — no quest hooks, no proper names, no plot, one sentence each.
Output ONLY valid JSON, no markdown: {"lines":["...","..."]} with exactly %d distinct lines.`, req.Count)

	userPrompt := fmt.Sprintf("Archetype: %s\nFaction/race: %s\nLocation/region: %s\nNotes: %s",
		req.Archetype, req.Faction, req.Region, req.Notes)

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

	lines, err := parseAmbientLines(gResp.Candidates[0].Content.Parts[0].Text)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ambientResult{
		Key:   ambientc.Key(req.Archetype, req.Faction, req.Region),
		Lines: lines,
	})
}

// parseAmbientLines extracts the cleaned, non-empty line list from the model's
// JSON output. Split out so it can be unit-tested without a live model.
func parseAmbientLines(raw string) ([]string, error) {
	var parsed struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(stripJSONFences(raw)), &parsed); err != nil {
		return nil, fmt.Errorf("LLM output invalid JSON: %w", err)
	}
	var lines []string
	for _, l := range parsed.Lines {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("LLM output had no usable lines")
	}
	return lines, nil
}
