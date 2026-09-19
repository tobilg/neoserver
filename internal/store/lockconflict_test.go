package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsLockConflict(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{"set lock", errors.New(`IO Error: Could not set lock on file "/data/neoserver.db": Conflicting lock is held`), true},
		{"conflicting lock", errors.New("Conflicting lock is held in PID 1234"), true},
		{"already open", errors.New("File is already open in another process"), true},
		{"database locked", errors.New("database is locked"), true},
		{"case insensitive", errors.New("COULD NOT SET LOCK ON FILE x"), true},
		{"wrapped", fmt.Errorf("attach: %w", errors.New("Conflicting lock is held")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLockConflict(tt.err); got != tt.want {
				t.Errorf("IsLockConflict(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestWrapAttachError(t *testing.T) {
	// Non-conflict errors pass through unchanged.
	plain := errors.New("wrong encryption key")
	if got := WrapAttachError(plain, "/x.db"); got != plain {
		t.Fatalf("non-conflict error must pass through, got %v", got)
	}
	if WrapAttachError(nil, "/x.db") != nil {
		t.Fatal("nil must stay nil")
	}

	// Lock conflicts get the single-active guidance, keep the original
	// wrapped, and name the path.
	conflict := errors.New("Conflicting lock is held")
	wrapped := WrapAttachError(conflict, "/srv/neoserver.db")
	if wrapped == nil || !errors.Is(wrapped, conflict) {
		t.Fatalf("original error not wrapped: %v", wrapped)
	}
	message := wrapped.Error()
	for _, want := range []string{"/srv/neoserver.db", "single active node", "another process"} {
		if !strings.Contains(message, want) {
			t.Errorf("message %q missing %q", message, want)
		}
	}
}
