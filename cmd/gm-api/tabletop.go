package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type diceReq struct {
	Formula string `json:"formula"`
}

type fowReq struct {
	Enabled bool `json:"enabled"`
}

type npcReq struct {
	Target  string `json:"target"`
	Command string `json:"command"`
}

func (s *server) rollDice(w http.ResponseWriter, r *http.Request) {
	var req diceReq
	if !decode(w, r, &req) {
		return
	}

	resultStr := parseAndRoll(req.Formula)

	// In a real scenario, we might broadcast this to the game via NATS
	// s.nats.Publish("gm.tabletop.dice", resultPayload)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"result": resultStr})
}

func (s *server) toggleFOW(w http.ResponseWriter, r *http.Request) {
	var req fowReq
	if !decode(w, r, &req) {
		return
	}

	// Broadcast to game clients to hide/reveal map
	payload, _ := json.Marshal(map[string]bool{"fog_of_war": req.Enabled})
	s.nats.Publish("gm.tabletop.fow", payload)

	w.WriteHeader(http.StatusOK)
}

func (s *server) npcCommand(w http.ResponseWriter, r *http.Request) {
	var req npcReq
	if !decode(w, r, &req) {
		return
	}

	// Tell EGS dynamic scenario service to manually override an NPC
	payload, _ := json.Marshal(req)
	s.nats.Publish("gm.tabletop.npc_command", payload)

	w.WriteHeader(http.StatusOK)
}

func parseAndRoll(formula string) string {
	if formula == "" {
		return "Invalid"
	}
	// e.g. "2d6 + 3" or "1d20"
	re := regexp.MustCompile(`(?i)(\d+)d(\d+)(?:\s*\+\s*(\d+))?`)
	matches := re.FindStringSubmatch(formula)
	if len(matches) < 3 {
		return fmt.Sprintf("Rolled %s: %d", formula, rand.Intn(20)+1)
	}

	count, _ := strconv.Atoi(matches[1])
	sides, _ := strconv.Atoi(matches[2])
	bonus := 0
	if len(matches) > 3 && matches[3] != "" {
		bonus, _ = strconv.Atoi(matches[3])
	}

	if count > 100 { count = 100 }
	if sides > 1000 { sides = 1000 }

	total := 0
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < count; i++ {
		total += rand.Intn(sides) + 1
	}
	total += bonus

	return fmt.Sprintf("Rolled %s = %d", formula, total)
}
