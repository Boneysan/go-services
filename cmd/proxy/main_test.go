package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakePub struct {
	subject string
	data    []byte
}

func (f *fakePub) Publish(subject string, data []byte) error {
	f.subject, f.data = subject, data
	return nil
}

func TestReloadSheetsPublishesFullBrickInvalidation(t *testing.T) {
	pub := &fakePub{}
	srv := &server{nats: pub}

	req := httptest.NewRequest(http.MethodPost, "/admin/reload-sheets", nil)
	w := httptest.NewRecorder()
	srv.reloadSheets(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusAccepted, w.Body)
	}
	if pub.subject != "sheet.updated.all" {
		t.Fatalf("subject = %q, want sheet.updated.all", pub.subject)
	}
	var payload map[string]string
	if err := json.Unmarshal(pub.data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["table"] != "bricks" || payload["sheet_id"] != "*" {
		t.Fatalf("payload = %#v, want full brick reload", payload)
	}
}
