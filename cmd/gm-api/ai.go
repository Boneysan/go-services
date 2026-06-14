package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/questc"
)

// geminiHTTP bounds every Gemini call so a slow/hung request can't pin a goroutine.
var geminiHTTP = &http.Client{Timeout: 60 * time.Second}

func geminiURL(model, key string) string {
	if model == "" {
		model = "gemini-1.5-flash"
	}
	return "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent?key=" + key
}

// buildGeminiRequest assembles a single-turn request with a system instruction.
func buildGeminiRequest(userText, systemText string) geminiRequest {
	var gReq geminiRequest
	gReq.Contents = append(gReq.Contents, struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{Parts: []struct {
		Text string `json:"text"`
	}{{Text: userText}}})
	gReq.SystemInstruction = &struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{Parts: []struct {
		Text string `json:"text"`
	}{{Text: systemText}}}
	return gReq
}

// stripJSONFences removes ```json / ``` fences an LLM may wrap around JSON output.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

type generateQuestReq struct {
	Prompt string `json:"prompt"`
}

type geminiRequest struct {
	Contents []struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"contents"`
	SystemInstruction *struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"system_instruction,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (s *server) generateQuest(w http.ResponseWriter, r *http.Request) {
	var req generateQuestReq
	if !decode(w, r, &req) {
		return
	}
	if req.Prompt == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "prompt is required")
		return
	}

	if s.geminiKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "service_unavailable", "GEMINI_API_KEY is not configured")
		return
	}

	systemPrompt := `You are an expert game designer for the MMORPG Ryzom. You will be provided with a prompt to generate a quest.
You must output ONLY valid JSON matching the 'scenario-graph/v1' schema. Do not output any markdown formatting, markdown code blocks, or explanatory text. The output must start with { and end with }.

The schema structure is as follows:
{
  "schema": "scenario-graph/v1",
  "id": "unique_storyline_id",
  "name": "Storyline Name",
  "start_quest": "quest_1",
  "quests": [
    {
      "id": "quest_1",
      "name": "Quest Name",
      "objectives": [
        {
          "id": "obj_1",
          "text": "Objective description",
          "trigger": { "on": "talk|kill|reach|collect|enter_zone|survive", "npc": "...", "count": 1 },
          "consequences": [
            { "action": "spawn|give_item|xp|faction|world_flag|message", "amount": 10 }
          ],
          "choice": {
             "prompt": "What do you do?",
             "mode": "initiator",
             "options": [
               { "id": "opt_1", "text": "Option 1", "next_quest": "quest_2" },
               { "id": "opt_2", "text": "Option 2", "next_quest": "" }
             ]
          }
        }
      ]
    }
  ]
}

Available triggers: "talk", "kill", "reach", "collect", "enter_zone", "survive".
Available consequences: "spawn", "give_item", "xp", "faction", "world_flag", "message".

Ensure all 'next_quest' references match valid quest IDs in the JSON. Output only JSON.`

	gReq := buildGeminiRequest(req.Prompt, systemPrompt)

	body, _ := json.Marshal(gReq)
	url := geminiURL(s.geminiModel, s.geminiKey)

	resp, err := geminiHTTP.Post(url, "application/json", bytes.NewReader(body))
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

	generatedText := stripJSONFences(gResp.Candidates[0].Content.Parts[0].Text)

	var storyline questc.Storyline
	if err := json.Unmarshal([]byte(generatedText), &storyline); err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", "LLM output invalid JSON: "+err.Error())
		return
	}

	if err := storyline.Validate(); err != nil {
		writeErr(w, http.StatusInternalServerError, "ai_error", "LLM output invalid scenario graph: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, storyline)
}
