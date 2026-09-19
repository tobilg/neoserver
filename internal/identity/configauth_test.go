package identity

import (
	"context"
	"net/http"
	"testing"
)

func TestStaticAPIKeyValidator(t *testing.T) {
	v := NewStaticAPIKeyValidator("s3cr3t-key", "super_admin")
	ctx := context.Background()

	t.Run("valid key via X-API-Key", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "s3cr3t-key")
		id, err := v.ValidateRequest(ctx, r)
		if err != nil || id == nil {
			t.Fatalf("expected identity, got id=%v err=%v", id, err)
		}
		if id.Roles["*"] != "super_admin" {
			t.Errorf("expected global super_admin role, got %v", id.Roles)
		}
	})

	t.Run("valid key via ApiKey scheme", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "ApiKey s3cr3t-key")
		id, err := v.ValidateRequest(ctx, r)
		if err != nil || id == nil {
			t.Fatalf("expected identity, got id=%v err=%v", id, err)
		}
	})

	t.Run("wrong key returns no identity", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "nope")
		id, err := v.ValidateRequest(ctx, r)
		if id != nil {
			t.Errorf("expected no identity for wrong key, got %v", id)
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("store key (nsk_) ignored", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "nsk_abc")
		id, _ := v.ValidateRequest(ctx, r)
		if id != nil {
			t.Errorf("expected nsk_ keys to be ignored by static validator, got %v", id)
		}
	})

	t.Run("no key configured is inert", func(t *testing.T) {
		empty := NewStaticAPIKeyValidator("", "super_admin")
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set("X-API-Key", "anything")
		id, err := empty.ValidateRequest(ctx, r)
		if id != nil || err != nil {
			t.Errorf("expected inert validator, got id=%v err=%v", id, err)
		}
	})
}

func TestBasicAuthValidator(t *testing.T) {
	v := NewBasicAuthValidator(map[string]string{"alice": "pw1"}, "super_admin")
	ctx := context.Background()

	t.Run("valid credentials", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.SetBasicAuth("alice", "pw1")
		id, err := v.ValidateRequest(ctx, r)
		if err != nil || id == nil {
			t.Fatalf("expected identity, got id=%v err=%v", id, err)
		}
		if id.Subject != "alice" || id.Roles["*"] != "super_admin" {
			t.Errorf("unexpected identity: %+v", id)
		}
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.SetBasicAuth("alice", "bad")
		id, err := v.ValidateRequest(ctx, r)
		if id != nil {
			t.Errorf("expected no identity, got %v", id)
		}
		if err == nil {
			t.Error("expected an auth error for bad password")
		}
	})

	t.Run("unknown user rejected", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		r.SetBasicAuth("mallory", "pw1")
		id, err := v.ValidateRequest(ctx, r)
		if id != nil || err == nil {
			t.Errorf("expected rejection, got id=%v err=%v", id, err)
		}
	})

	t.Run("no basic header is inert", func(t *testing.T) {
		r, _ := http.NewRequest("GET", "/", nil)
		id, err := v.ValidateRequest(ctx, r)
		if id != nil || err != nil {
			t.Errorf("expected inert, got id=%v err=%v", id, err)
		}
	})
}
