package stylegraphics

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestImmutableObjectsAndVerifiedLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	body := []byte("original")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, "marker.svg"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAssetObject(dir, "marker.svg", digest, 1024); err != nil {
		t.Fatal(err)
	}
	if err := WriteAssetObject(dir, digest, body); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker.svg"), []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	// An older snapshot keeps reading the exact object it names.
	if got, err := ReadAssetObject(dir, "marker.svg", digest, 1024); err != nil || string(got) != "original" {
		t.Fatalf("read: %q %v", got, err)
	}
	other := sha256.Sum256([]byte("unrelated"))
	if _, err := ReadAssetObject(dir, "marker.svg", hex.EncodeToString(other[:]), 1024); err == nil {
		t.Fatal("mismatching legacy payload accepted")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if err := WriteAssetObject(dir, digest, body); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	entries, err := os.ReadDir(filepath.Join(dir, ".objects"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != digest {
		t.Fatalf("temporary payloads leaked: %v", entries)
	}
}
