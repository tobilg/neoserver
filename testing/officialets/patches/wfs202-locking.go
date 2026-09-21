// Correct two locking-test defects in ets-wfs20 1.42: the response-element
// lookup and the use of sampled IDs after destructive transaction tests.
// No assertions, test selection, or skip rules are changed.
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

const targetSuite = "org/opengis/cite/iso19142/testng.xml"
const expectedSuiteSHA256 = "ae389de80f6ce15b3d77a575ad74a7ad3611364e5740d56e3b88b47740c58c43"

const transactionTests = `  <test name="Transactional WFS">
    <classes>
      <class name="org.opengis.cite.iso19142.transaction.TransactionCapabilitiesTests" />
      <class name="org.opengis.cite.iso19142.transaction.Update" />
      <class name="org.opengis.cite.iso19142.transaction.InsertTests" />
      <class name="org.opengis.cite.iso19142.transaction.ReplaceTests" />
      <class name="org.opengis.cite.iso19142.transaction.DeleteTests" />
    </classes>
  </test>
`

func patchSuite(input []byte) ([]byte, error) {
	// DeleteTests restores deleted features by inserting them. Their new IDs
	// are absent from DataSampler's initial snapshot, so subsequent locking
	// tests can randomly select deleted IDs. Finish all snapshot consumers
	// before running the unchanged transaction group.
	group := []byte(transactionTests)
	end := []byte("</suite>")
	if bytes.Count(input, group) != 1 || bytes.Count(input, end) != 1 {
		return nil, fmt.Errorf("unexpected WFS test suite structure")
	}
	output := bytes.Replace(input, group, nil, 1)
	return bytes.Replace(output, end, append(group, end...), 1), nil
}

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
	patchedClass, patchedSuite := false, false
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
		switch file.Name {
		case targetClass:
			// Input is tied to the pinned 1.42 image, not a permissive text search.
			if fmt.Sprintf("%x", sha256.Sum256(body)) != expectedClassSHA256 {
				return fmt.Errorf("unexpected stock class checksum")
			}
			body, err = patchClass(body)
			if err != nil {
				return err
			}
			patchedClass = true
		case targetSuite:
			if fmt.Sprintf("%x", sha256.Sum256(body)) != expectedSuiteSHA256 {
				return fmt.Errorf("unexpected stock suite checksum")
			}
			body, err = patchSuite(body)
			if err != nil {
				return err
			}
			patchedSuite = true
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			return err
		}
		if _, err = entry.Write(body); err != nil {
			return err
		}
	}
	if !patchedClass || !patchedSuite {
		return fmt.Errorf("LockFeatureTests class or WFS test suite not found")
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, output.Bytes(), 0644)
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: wfs202-locking <suite.jar>")
	}
	if err := patchJar(os.Args[1]); err != nil {
		panic(err)
	}
}
