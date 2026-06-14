package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type NPCContext struct {
	NPCIdentity  string   `json:"identity"`    // name, race, faction
	NPCLocation  string   `json:"location"`    // current zone
	QuestState   string   `json:"quest_state"` // player's relevant quest flags
	PlayerRep    int      `json:"reputation"`  // bucketed: hostile/neutral/friendly/ally
	RecentEvents []string `json:"recent_events"`
}

type DialogueRequest struct {
	PlayerID   int        `json:"player_id"`
	PlayerText string     `json:"player_text"`
	Context    NPCContext `json:"context"`
}

type DialogueResponse struct {
	Text     string `json:"text"`
	Subtitle string `json:"subtitle"`
}

var (
	dialogueCache = make(map[string]DialogueResponse)
	dialogueMu    sync.RWMutex
)

// buildContextHash keys the cache on the NPC context AND the player's input —
// different player utterances to the same NPC must not collide.
func buildContextHash(ctx NPCContext, playerText string) string {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s|%s|%s|%d|%s", ctx.NPCIdentity, ctx.NPCLocation, ctx.QuestState, ctx.PlayerRep, playerText)))
	return hex.EncodeToString(hasher.Sum(nil))
}

// Simulated Claude Haiku Call
func callClaudeHaiku(ctx NPCContext, playerText string) (DialogueResponse, error) {
	// Simulate network latency for LLM call
	time.Sleep(300 * time.Millisecond)

	return DialogueResponse{
		Text:     "Greetings, traveler. I sense you carry the mark of the Kami.",
		Subtitle: "Greetings, traveler. I sense you carry the mark of the Kami.",
	}, nil
}

func handleDialogueRequest(w http.ResponseWriter, r *http.Request) {
	var req DialogueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	hash := buildContextHash(req.Context, req.PlayerText)
	dialogueMu.RLock()
	cachedRes, ok := dialogueCache[hash]
	dialogueMu.RUnlock()
	if ok {
		slog.Info("LLM Cache hit", "hash", hash)
		json.NewEncoder(w).Encode(cachedRes)
		return
	}

	res, err := callClaudeHaiku(req.Context, req.PlayerText)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dialogueMu.Lock()
	dialogueCache[hash] = res
	dialogueMu.Unlock()
	json.NewEncoder(w).Encode(res)
}

func main() {
	nc, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		slog.Error("Failed to connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	// Subscribe to dialogue requests from NATS
	nc.Subscribe("ai.dialogue.request", func(m *nats.Msg) {
		var req DialogueRequest
		if err := json.Unmarshal(m.Data, &req); err == nil {
			hash := buildContextHash(req.Context, req.PlayerText)
			dialogueMu.RLock()
			res, ok := dialogueCache[hash]
			dialogueMu.RUnlock()
			if !ok {
				res, _ = callClaudeHaiku(req.Context, req.PlayerText)
				dialogueMu.Lock()
				dialogueCache[hash] = res
				dialogueMu.Unlock()
			}
			resData, _ := json.Marshal(res)
			m.Respond(resData)
		}
	})

	http.HandleFunc("/ai/dialogue", handleDialogueRequest)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	slog.Info("Starting AI API server", "port", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		slog.Error("Server failed", "err", err)
		os.Exit(1)
	}
}
