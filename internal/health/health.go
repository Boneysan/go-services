package health

import (
	"encoding/json"
	"net/http"
	"time"
)

var startTime = time.Now()

type Response struct {
	Status   string            `json:"status"`
	UptimeS  int64             `json:"uptime_s"`
	Extra    map[string]string `json:"extra,omitempty"`
}

func Handler(extra map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			Status:  "ok",
			UptimeS: int64(time.Since(startTime).Seconds()),
			Extra:   extra,
		})
	}
}
