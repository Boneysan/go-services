package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakePub struct {
	subject string
	data    []byte
}

func (f *fakePub) Publish(subject string, data []byte) error {
	f.subject, f.data = subject, data
	return nil
}

func newTestServer(token string) (*server, *fakePub) {
	pub := &fakePub{}
	return &server{nats: pub, token: token, start: time.Now()}, pub
}

func do(t *testing.T, h http.HandlerFunc, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	// register with a pattern so PathValue works
	switch method {
	case "PATCH", "DELETE":
		mux.HandleFunc(method+" /gm/entities/{entity_id}", h)
	default:
		mux.HandleFunc(method+" "+path, h)
	}
	mux.ServeHTTP(w, req)
	return w
}

func TestAuth(t *testing.T) {
	srv, _ := newTestServer("sekrit")
	h := srv.auth(srv.scriptRun)

	if w := do(t, h, "POST", "/gm/script/run", `{"lua":"x()"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d, want 401", w.Code)
	}
	if w := do(t, h, "POST", "/gm/script/run", `{"lua":"x()"}`,
		map[string]string{"Authorization": "Bearer wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", w.Code)
	}
	if w := do(t, h, "POST", "/gm/script/run", `{"lua":"x()"}`,
		map[string]string{"Authorization": "Bearer sekrit"}); w.Code != http.StatusAccepted {
		t.Fatalf("good token: got %d, want 202", w.Code)
	}

	open, _ := newTestServer("") // dev mode: no token configured
	if w := do(t, open.auth(open.scriptRun), "POST", "/gm/script/run", `{"lua":"x()"}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("dev mode: got %d, want 202", w.Code)
	}
}

func TestSpawnValidationAndEnvelope(t *testing.T) {
	srv, pub := newTestServer("")

	if w := do(t, srv.spawn, "POST", "/gm/spawn", `{"zone_id":"z"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing sheet_id/position: got %d, want 400", w.Code)
	}
	if w := do(t, srv.spawn, "POST", "/gm/spawn",
		`{"sheet_id":"kizoar","position":{"x":1,"y":2,"z":3},"count":99}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("count 99: got %d, want 400", w.Code)
	}
	if w := do(t, srv.spawn, "POST", "/gm/spawn",
		`{"sheet_id":"kizoar","position":{"x":1,"y":2,"z":3},"level":50}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("valid spawn: got %d, want 202: %s", w.Code, w.Body)
	}
	if pub.subject != "gm.spawn" {
		t.Fatalf("subject = %q, want gm.spawn", pub.subject)
	}

	var env envelope
	if err := json.Unmarshal(pub.data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Command != "spawn" || env.IssuedAt.IsZero() {
		t.Fatalf("bad envelope: %+v", env)
	}
	var req spawnReq
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		t.Fatal(err)
	}
	if req.SheetID != "kizoar" || req.Count != 1 || req.Level == nil || *req.Level != 50 {
		t.Fatalf("payload defaults wrong: %+v", req)
	}
}

func TestEntityPatchAndDespawn(t *testing.T) {
	srv, pub := newTestServer("")

	if w := do(t, srv.patchEntity, "PATCH", "/gm/entities/e42", `{"hp":9999}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("patch: got %d, want 202", w.Code)
	}
	if pub.subject != "gm.entity.patch" || !strings.Contains(string(pub.data), `"entity_id":"e42"`) {
		t.Fatalf("patch publish wrong: %s %s", pub.subject, pub.data)
	}
	if w := do(t, srv.patchEntity, "PATCH", "/gm/entities/e42", `{}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty patch: got %d, want 400", w.Code)
	}

	if w := do(t, srv.despawnEntity, "DELETE", "/gm/entities/e42", "", nil); w.Code != http.StatusAccepted {
		t.Fatalf("despawn: got %d, want 202", w.Code)
	}
	if pub.subject != "gm.entity.despawn" {
		t.Fatalf("despawn subject = %q", pub.subject)
	}
}

func TestPartyManagementAndTeleport(t *testing.T) {
	srv, pub := newTestServer("")

	// join_party — requires char_id + party_id
	if w := do(t, srv.joinParty, "POST", "/gm/party/join", `{"char_id":"c1"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing party_id: got %d, want 400", w.Code)
	}
	if w := do(t, srv.joinParty, "POST", "/gm/party/join", `{"char_id":"c1","party_id":"p1"}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("join_party: got %d, want 202: %s", w.Code, w.Body)
	}
	if pub.subject != "gm.join_party" {
		t.Fatalf("join_party subject = %q, want gm.join_party", pub.subject)
	}

	// leave_party — requires char_id
	if w := do(t, srv.leaveParty, "POST", "/gm/party/leave", `{}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing char_id: got %d, want 400", w.Code)
	}
	if w := do(t, srv.leaveParty, "POST", "/gm/party/leave", `{"char_id":"c1"}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("leave_party: got %d, want 202: %s", w.Code, w.Body)
	}

	// set_anchor — requires party_id
	if w := do(t, srv.setAnchor, "POST", "/gm/party/anchor", `{"x":100,"y":200}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing party_id: got %d, want 400", w.Code)
	}
	if w := do(t, srv.setAnchor, "POST", "/gm/party/anchor", `{"party_id":"p1","x":100,"y":200,"z":5}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("set_anchor: got %d, want 202: %s", w.Code, w.Body)
	}
	if pub.subject != "gm.set_anchor" {
		t.Fatalf("set_anchor subject = %q, want gm.set_anchor", pub.subject)
	}

	// gmTeleport — requires char_id
	if w := do(t, srv.gmTeleport, "POST", "/gm/teleport", `{"x":10,"y":20}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing char_id: got %d, want 400", w.Code)
	}
	if w := do(t, srv.gmTeleport, "POST", "/gm/teleport", `{"char_id":"c1","x":10,"y":20,"z":3}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("teleport: got %d, want 202: %s", w.Code, w.Body)
	}
	if pub.subject != "gm.teleport" {
		t.Fatalf("teleport subject = %q, want gm.teleport", pub.subject)
	}
}

func TestWeatherAndEventAndScript(t *testing.T) {
	srv, pub := newTestServer("")

	if w := do(t, srv.weather, "POST", "/gm/weather", `{"zone_id":"z","weather":"hail"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad weather type: got %d, want 400", w.Code)
	}
	if w := do(t, srv.weather, "POST", "/gm/weather", `{"zone_id":"z","weather":"storm"}`, nil); w.Code != http.StatusAccepted {
		t.Fatalf("storm: got %d, want 202", w.Code)
	}
	if pub.subject != "gm.weather" {
		t.Fatalf("subject = %q", pub.subject)
	}

	if w := do(t, srv.eventTrigger, "POST", "/gm/event/trigger", `{"zone":"z"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("missing name: got %d, want 400", w.Code)
	}
	if w := do(t, srv.scriptRun, "POST", "/gm/script/run", `{"lua":"  "}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("blank lua: got %d, want 400", w.Code)
	}
	if w := do(t, srv.scriptRun, "POST", "/gm/script/run", `{"lua":"spawn_boss()","extra":1}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: got %d, want 400", w.Code)
	}
}
