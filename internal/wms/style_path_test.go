package wms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeStylePath(t *testing.T) {
	root := t.TempDir()
	style := filepath.Join(root, "roads.sld")
	if err := os.WriteFile(style, []byte("style"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := safeStylePath(root, "roads")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(style)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != expected {
		t.Fatalf("got %q", resolved)
	}
	if _, err := safeStylePath(root, "../secret"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestSafeStylePathRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "secret.sld")
	if err := os.WriteFile(target, []byte("style"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked.sld")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeStylePath(root, "linked"); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}
