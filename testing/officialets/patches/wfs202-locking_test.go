package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionsRunAfterSnapshotConsumersWithoutChangingTests(t *testing.T) {
	before := `<suite><test name="Basic WFS"><classes><class name="BasicTests"/></classes></test>`
	after := `<test name="Locking WFS"><classes><class name="LockFeatureTests"/></classes></test><test name="Response paging"/>`
	input := []byte(before + transactionTests + after + "</suite>")
	got, err := patchSuite(input)
	if err != nil {
		t.Fatal(err)
	}
	if want := before + after + transactionTests + "</suite>"; string(got) != want {
		t.Fatalf("patch must only move the intact transaction group to the end: %s", got)
	}
	var suite struct {
		Tests []struct {
			Name string `xml:"name,attr"`
		} `xml:"test"`
	}
	if err := xml.Unmarshal(got, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Tests) != 4 || suite.Tests[3].Name != "Transactional WFS" {
		t.Fatalf("unexpected test sequence: %+v", suite.Tests)
	}
	for _, invalid := range [][]byte{nil, []byte(before + after + "</suite>"), append(input, []byte(transactionTests)...)} {
		if _, err := patchSuite(invalid); err == nil {
			t.Fatal("unexpected suite structure accepted")
		}
	}
}

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

func TestPatchRejectsUnsupportedJarsWithoutChangingThem(t *testing.T) {
	for _, name := range []string{targetClass, targetSuite, "unrelated.txt"} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			w := zip.NewWriter(&data)
			entry, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write([]byte("unexpected contents")); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "suite.jar")
			if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := patchJar(path); err == nil {
				t.Fatal("unsupported JAR accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, data.Bytes()) {
				t.Fatal("failed patch changed input")
			}
		})
	}
}
