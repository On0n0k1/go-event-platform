package analytics

import (
	"net/http"

	"github.com/On0n0k1/go-event-platform/analytics-service/internal/httpx"
)

func StatsHandler(stats *Stats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, stats.Snapshot())
	}
}
