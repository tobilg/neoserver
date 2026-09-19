package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const readinessTimeout = 2 * time.Second

type readinessCheck struct {
	name    string
	enabled bool
	check   func(context.Context) error
}

type readinessResponse struct {
	Status      string             `json:"status"`
	Checks      map[string]string  `json:"checks"`
	DurationsMS map[string]float64 `json:"durations_ms"`
}

// HealthHandler is intentionally dependency-free: reaching it proves the HTTP
// process is alive, even when durable dependencies are unavailable.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}
}

func ReadinessHandler(logger *slog.Logger, checks []readinessCheck) http.HandlerFunc {
	return readinessHandler(logger, checks, readinessTimeout)
}

func readinessHandler(logger *slog.Logger, checks []readinessCheck, timeout time.Duration) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		statuses := make(map[string]string, len(checks))
		durations := make(map[string]float64, len(checks))
		type result struct {
			name     string
			err      error
			duration time.Duration
		}
		results := make(chan result, len(checks))
		enabled := 0
		for _, item := range checks {
			if !item.enabled {
				statuses[item.name] = "disabled"
				durations[item.name] = 0
				continue
			}
			enabled++
			go func(item readinessCheck) {
				started := time.Now()
				results <- result{name: item.name, err: item.check(ctx), duration: time.Since(started)}
			}(item)
		}
		failed := false
		for received := 0; received < enabled; {
			select {
			case item := <-results:
				received++
				durations[item.name] = float64(item.duration.Microseconds()) / 1000
				if item.err != nil {
					statuses[item.name] = "failed"
					failed = true
					logger.Error("readiness check failed", "component", item.name, "error", item.err)
				} else {
					statuses[item.name] = "ok"
				}
			case <-ctx.Done():
				for _, item := range checks {
					if item.enabled {
						if _, reported := statuses[item.name]; !reported {
							statuses[item.name] = "failed"
							durations[item.name] = float64(timeout.Microseconds()) / 1000
							logger.Error("readiness check timed out", "component", item.name, "error", ctx.Err())
						}
					}
				}
				failed = true
				received = enabled
			}
		}
		response := readinessResponse{Status: "ready", Checks: statuses, DurationsMS: durations}
		statusCode := http.StatusOK
		if failed {
			response.Status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(response)
		}
	}
}
