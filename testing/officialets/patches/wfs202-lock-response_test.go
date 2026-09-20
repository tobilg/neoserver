package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchChangesOnlyExpectedConstant(t *testing.T) {
	constant := append([]byte{1, 0, 17}, []byte("FeatureCollection")...)
	input := append([]byte("prefix"), constant...)
	input = append(input, []byte("suffix")...)
	got, err := patchClass(input)
	want := append([]byte("prefix"), append([]byte{1, 0, 19}, []byte("LockFeatureResponse")...)...)
	want = append(want, []byte("suffix")...)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("patched constant = %q, %v", got, err)
	}
	for _, invalid := range [][]byte{nil, append(constant, constant...), got} {
		if _, err := patchClass(invalid); err == nil {
			t.Fatal("unexpected input was accepted")
		}
	}
}

func TestPatchRejectsUnknownJarWithoutChangingIt(t *testing.T) {
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	entry, err := w.Create(targetClass)
	if err != nil {
		t.Fatal(err)
	}
	entry.Write([]byte("different class"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "suite.jar")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := patchJar(path); err == nil {
		t.Fatal("unknown class checksum accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data.Bytes()) {
		t.Fatal("failed patch changed input")
	}
}
