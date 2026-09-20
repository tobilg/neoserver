// Patch only the incorrect response-element lookup in ets-wfs20 1.42.
// Its LockFeature test searches for FeatureCollection, dereferences null, and
// leaks a lock before it can exercise lockId plus query rejection (2.0.2).
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

const expectedClassSHA256 = "cfbbbfc53ed5c91b8f891456bc9bfff434a11f1948f959e15beae10e105eab12"

const targetClass = "org/opengis/cite/iso19142/locking/LockFeatureTests.class"

func patchClass(input []byte) ([]byte, error) {
	old := append([]byte{1, 0, 17}, []byte("FeatureCollection")...) // CONSTANT_Utf8
	next := append([]byte{1, 0, 19}, []byte("LockFeatureResponse")...)
	if bytes.Count(input, old) != 1 {
		return nil, fmt.Errorf("unexpected LockFeatureTests constant pool")
	}
	return bytes.Replace(input, old, next, 1), nil
}

func patchJar(path string) error {
	input, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer input.Close()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	patched := false
	for _, file := range input.File {
		reader, err := file.Open()
		if err != nil {
			return err
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return err
		}
		if file.Name == targetClass {
			// Input is tied to the pinned 1.42 image, not a permissive text search.
			if fmt.Sprintf("%x", sha256.Sum256(body)) != expectedClassSHA256 {
				return fmt.Errorf("unexpected stock class checksum")
			}
			body, err = patchClass(body)
			if err != nil {
				return err
			}
			patched = true
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			return err
		}
		if _, err = entry.Write(body); err != nil {
			return err
		}
	}
	if !patched {
		return fmt.Errorf("LockFeatureTests class not found")
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, output.Bytes(), 0644)
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: wfs202-lock-response <suite.jar>")
	}
	if err := patchJar(os.Args[1]); err != nil {
		panic(err)
	}
}
