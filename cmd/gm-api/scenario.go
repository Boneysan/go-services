package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/boneysan/ryzom/go-services/internal/questc"
)

func (s *server) scenarioImport(w http.ResponseWriter, r *http.Request) {
	var storyline questc.Storyline
	if err := json.NewDecoder(r.Body).Decode(&storyline); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "Failed to parse scenario JSON")
		return
	}

	if err := storyline.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "validation_error", err.Error())
		return
	}

	luaScript, err := questc.Compile(&storyline)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "compile_error", err.Error())
		return
	}

	// Wrap the compiled script so it both registers and immediately starts the
	// scenario in the EGS Lua state.  The compiled script ends with "return story",
	// but gm.script.run discards return values; we capture it via an IIFE instead.
	wrapped := fmt.Sprintf(
		"local _s=(function()\n%s\nend)()\n"+
			"local _d=package.loaded['dss_scenario_host']\n"+
			"if _d then _d.start_scenario(_s)\n"+
			"else egs.warning('dss_scenario_host not loaded') end\n",
		luaScript,
	)

	// Publish using the standard gm.* envelope so the EGS NATS subscriber
	// recognises the "command" field and routes to the script_run handler.
	env, _ := json.Marshal(map[string]any{
		"command": "script_run",
		"lua":     wrapped,
	})
	if err := s.nats.Publish("gm.script.run", env); err != nil {
		writeErr(w, http.StatusBadGateway, "nats_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "imported",
		"id":     storyline.ID,
		"name":   storyline.Name,
	})
}

func (s *server) scenarioStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	s.publish(w, "gm.start_scenario", "start_scenario", req)
}

func (s *server) fireEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string `json:"kind"`
		Target string `json:"target,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Kind == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "kind is required")
		return
	}
	s.publish(w, "gm.fire_event", "fire_event", req)
}
