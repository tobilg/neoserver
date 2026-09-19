package tilecache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesystemStorePathTraversalRejected(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, key := range []string{
		"..",
		"../escape",
		"../../etc/passwd",
		"/absolute/path",
		"a/../../escape",
		".",
		"",
	} {
		if err := store.Put(ctx, key, []byte("x")); err == nil {
			t.Errorf("Put(%q) should fail", key)
		}
		if _, err := store.Get(ctx, key); err == nil {
			t.Errorf("Get(%q) should fail", key)
		}
		if err := store.Delete(ctx, key); err == nil {
			t.Errorf("Delete(%q) should fail", key)
		}
	}

	// Interior ".." that still resolves inside the root is allowed by Clean.
	if err := store.Put(ctx, "a/b/../c.png", []byte("x")); err != nil {
		t.Errorf("cleanable key rejected: %v", err)
	}
}

func TestFilesystemStoreAtomicWriteAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := store.Put(ctx, "z/1/2.png", []byte("tile-bytes")); err != nil {
		t.Fatalf("put: %v", err)
	}
	// No temp files remain after the atomic rename.
	entries, _ := os.ReadDir(filepath.Join(root, "z", "1"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tile-") {
			t.Errorf("temp file left behind: %s", entry.Name())
		}
	}

	reader, err := store.Get(ctx, "z/1/2.png")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	if string(body) != "tile-bytes" {
		t.Fatalf("body = %q", body)
	}

	// Overwrite replaces content atomically.
	if err := store.Put(ctx, "z/1/2.png", []byte("v2")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	reader, _ = store.Get(ctx, "z/1/2.png")
	body, _ = io.ReadAll(reader)
	reader.Close()
	if string(body) != "v2" {
		t.Fatalf("overwritten body = %q", body)
	}
}

func TestFilesystemStoreMissingAndDelete(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Get(ctx, "missing.png"); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Get missing = %v, want ErrBlobNotFound", err)
	}
	// Deleting a missing key is not an error.
	if err := store.Delete(ctx, "missing.png"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}

	if err := store.Put(ctx, "a.png", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteBatch(ctx, []string{"a.png", "missing.png"}); err != nil {
		t.Fatalf("DeleteBatch: %v", err)
	}
	if _, err := store.Get(ctx, "a.png"); !errors.Is(err, ErrBlobNotFound) {
		t.Fatal("a.png must be deleted")
	}

	if err := store.Health(ctx); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestFilesystemStoreCancelledContext(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Put(ctx, "a.png", []byte("x")); err == nil {
		t.Fatal("cancelled Put should fail")
	}
	if err := store.DeleteBatch(ctx, []string{"a.png"}); err == nil {
		t.Fatal("cancelled DeleteBatch should fail")
	}
}
