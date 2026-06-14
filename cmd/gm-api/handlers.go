package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/boneysan/ryzom/go-services/internal/natspub"
)

type server struct {
	nats        natspub.Publisher
	token       string
	geminiKey   string
	geminiModel string
	start       time.Time
}

// envelope is the wire format for every gm.* subject. The EGS subscriber
// uses issued_at to drop stale commands after a NATS outage replay.
type envelope struct {
	Command  string          `json:"command"`
	IssuedAt time.Time       `json:"issued_at"`
	Payload  json.RawMessage `json:"payload"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// auth enforces the GM bearer token when GM_API_TOKEN is configured.
// An empty configured token (local dev) lets everything through.
func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid GM token")
				return
			}
		}
		next(w, r)
	}
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"uptime_s": int64(time.Since(s.start).Seconds()),
	})
}

// publish wraps payload in the command envelope and emits it; the HTTP reply
// is 202 because execution happens asynchronously in the EGS.
func (s *server) publish(w http.ResponseWriter, subject, command string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal_error", err.Error())
		return
	}
	env, _ := json.Marshal(envelope{Command: command, IssuedAt: time.Now().UTC(), Payload: raw})
	if err := s.nats.Publish(subject, env); err != nil {
		writeErr(w, http.StatusBadGateway, "nats_error", err.Error())
		return
	}
	slog.Info("gm command published", "subject", subject)
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "subject": subject})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return false
	}
	return true
}

// --- command handlers -------------------------------------------------------

type position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type spawnReq struct {
	SheetID          string    `json:"sheet_id"`
	ZoneID           string    `json:"zone_id"`
	Position         *position `json:"position"`
	Level            *int      `json:"level,omitempty"`
	NameOverride     *string   `json:"name_override,omitempty"`
	AIScriptOverride *string   `json:"ai_script_override,omitempty"`
	Count            int       `json:"count,omitempty"`
}

func (s *server) spawn(w http.ResponseWriter, r *http.Request) {
	var req spawnReq
	if !decode(w, r, &req) {
		return
	}
	if req.SheetID == "" || req.Position == nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "sheet_id and position are required")
		return
	}
	if req.Count == 0 {
		req.Count = 1
	}
	if req.Count < 1 || req.Count > 50 {
		writeErr(w, http.StatusBadRequest, "bad_request", "count must be 1..50")
		return
	}
	s.publish(w, "gm.spawn", "spawn", req)
}

func (s *server) patchEntity(w http.ResponseWriter, r *http.Request) {
	var fields map[string]any
	if !decode(w, r, &fields) {
		return
	}
	if len(fields) == 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "no fields to patch")
		return
	}
	s.publish(w, "gm.entity.patch", "entity_patch", map[string]any{
		"entity_id": r.PathValue("entity_id"),
		"fields":    fields,
	})
}

func (s *server) despawnEntity(w http.ResponseWriter, r *http.Request) {
	s.publish(w, "gm.entity.despawn", "entity_despawn", map[string]string{
		"entity_id": r.PathValue("entity_id"),
	})
}

var weatherTypes = map[string]bool{"clear": true, "rain": true, "storm": true, "fog": true}

type weatherReq struct {
	ZoneID          string `json:"zone_id"`
	Weather         string `json:"weather"`
	TransitionTicks int    `json:"transition_ticks,omitempty"`
}

func (s *server) weather(w http.ResponseWriter, r *http.Request) {
	var req weatherReq
	if !decode(w, r, &req) {
		return
	}
	if !weatherTypes[req.Weather] {
		writeErr(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("weather must be one of clear|rain|storm|fog, got %q", req.Weather))
		return
	}
	s.publish(w, "gm.weather", "weather", req)
}

type eventReq struct {
	Name string `json:"name"`
	Zone string `json:"zone,omitempty"`
}

func (s *server) eventTrigger(w http.ResponseWriter, r *http.Request) {
	var req eventReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	s.publish(w, "gm.event.trigger", "event_trigger", req)
}

type scriptReq struct {
	Lua string `json:"lua"`
}

func (s *server) scriptRun(w http.ResponseWriter, r *http.Request) {
	var req scriptReq
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Lua) == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "lua is required")
		return
	}
	s.publish(w, "gm.script.run", "script_run", req)
}

type awardSkillReq struct {
	CharacterID string  `json:"character_id"`
	Skill       string  `json:"skill"`
	XPAmount    float64 `json:"xp_amount"`
}

func (s *server) awardSkill(w http.ResponseWriter, r *http.Request) {
	var req awardSkillReq
	if !decode(w, r, &req) {
		return
	}
	if req.CharacterID == "" || req.Skill == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "character_id and skill are required")
		return
	}
	if req.XPAmount <= 0 {
		writeErr(w, http.StatusBadRequest, "bad_request", "xp_amount must be greater than 0")
		return
	}
	s.publish(w, "gm.award.skill", "award_skill", req)
}
