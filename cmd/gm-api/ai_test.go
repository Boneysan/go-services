package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateQuestWithFakeGemini(t *testing.T) {
	// Create a fake Gemini server
	fakeGemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock response
		resp := geminiResponse{}
		resp.Candidates = append(resp.Candidates, struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}{
			Content: struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			}{
				Parts: []struct {
					Text string `json:"text"`
				}{
					{
						Text: `{
  "schema": "scenario-graph/v1",
  "id": "test_storyline",
  "name": "Test Storyline",
  "start_quest": "q1",
  "quests": [
    {
      "id": "q1",
      "name": "First Quest",
      "objectives": [
        {
          "id": "o1",
          "text": "Talk to the merchant",
          "trigger": { "on": "talk", "npc": "merchant" }
        }
      ]
    }
  ]
}`,
					},
				},
			},
		})
		json.NewEncoder(w).Encode(resp)
	}))
	defer fakeGemini.Close()

	// temporarily override gemini URL
	originalGeminiURL := geminiURL
	geminiURL = func(model, key string) string {
		return fakeGemini.URL
	}
	defer func() { geminiURL = originalGeminiURL }()

	// create the server instance
	s := &server{
		geminiKey:   "fake_key",
		geminiModel: "fake_model",
	}

	// Make the request
	reqBody := `{"prompt": "make a quest"}`
	req := httptest.NewRequest(http.MethodPost, "/generateQuest", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.generateQuest(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status OK, got %v", res.Status)
	}
}
