package datasource

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
)

func TestPreloadExtensionsLogging(t *testing.T) {
	const start = "preloading DuckDB extension"
	const complete = "preloaded DuckDB extension"
	type event struct {
		Level     string
		Message   string `json:"msg"`
		Extension string
		Duration  *float64 `json:"duration_ms"`
		Error     string
	}
	for _, tc := range []struct {
		name      string
		level     slog.Level
		failQuery string
		want      []event
	}{
		{
			name:  "info logs only completion",
			level: slog.LevelInfo,
			want: []event{
				{Level: "INFO", Message: complete, Extension: "spatial"},
				{Level: "INFO", Message: complete, Extension: "httpfs"},
			},
		},
		{
			name:  "debug also logs start",
			level: slog.LevelDebug,
			want: []event{
				{Level: "DEBUG", Message: start, Extension: "spatial"},
				{Level: "INFO", Message: complete, Extension: "spatial"},
				{Level: "DEBUG", Message: start, Extension: "httpfs"},
				{Level: "INFO", Message: complete, Extension: "httpfs"},
			},
		},
		{
			name:      "install failure still warns and continues",
			level:     slog.LevelInfo,
			failQuery: "INSTALL 'spatial';",
			want: []event{
				{Level: "WARN", Message: "failed to install extension", Extension: "spatial", Error: "test failure"},
				{Level: "INFO", Message: complete, Extension: "httpfs"},
			},
		},
		{
			name:      "load failure never logs completion",
			level:     slog.LevelDebug,
			failQuery: "LOAD 'spatial';",
			want: []event{
				{Level: "DEBUG", Message: start, Extension: "spatial"},
				{Level: "WARN", Message: "failed to load extension", Extension: "spatial", Error: "test failure"},
				{Level: "DEBUG", Message: start, Extension: "httpfs"},
				{Level: "INFO", Message: complete, Extension: "httpfs"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: tc.level}))
			ctx := t.Context()
			var queries []string
			preloadExtensions(ctx, logger, func(gotCtx context.Context, query string, args ...any) (sql.Result, error) {
				if gotCtx != ctx {
					t.Error("SQL execution did not receive the startup context")
				}
				queries = append(queries, query)
				if query == tc.failQuery {
					return nil, errors.New("test failure")
				}
				return nil, nil
			})
			wantQueries := []string{"INSTALL 'spatial';"}
			if tc.failQuery != "INSTALL 'spatial';" {
				wantQueries = append(wantQueries, "LOAD 'spatial';")
			}
			wantQueries = append(wantQueries, "INSTALL 'httpfs';", "LOAD 'httpfs';")
			if !slices.Equal(queries, wantQueries) {
				t.Fatalf("queries = %v, want %v", queries, wantQueries)
			}

			var events []event
			decoder := json.NewDecoder(&output)
			for {
				var logged event
				if err := decoder.Decode(&logged); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if logged.Message == complete {
					if logged.Duration == nil || *logged.Duration < 0 {
						t.Errorf("completion for %s must include a non-negative duration_ms: %+v", logged.Extension, logged)
					}
					logged.Duration = nil // Elapsed time is intentionally not deterministic.
				}
				events = append(events, logged)
			}
			if !slices.Equal(events, tc.want) {
				t.Errorf("events = %+v, want %+v", events, tc.want)
			}
		})
	}
}
