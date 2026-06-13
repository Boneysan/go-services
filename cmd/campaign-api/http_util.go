package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// maxBundleBytes caps an imported bundle (defensive; a campaign JSON is small).
const maxBundleBytes = 32 << 20 // 32 MiB

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	// Lenient on unknown fields: a portable archive should survive a future
	// schema adding fields. Schema/version checks happen in the handler.
	return json.NewDecoder(io.LimitReader(r.Body, maxBundleBytes)).Decode(v)
}
