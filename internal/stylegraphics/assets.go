package stylegraphics

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobilg/neoserver/internal/sld"
)

func validDigest(digest string) bool {
	bytes, err := hex.DecodeString(digest)
	return err == nil && len(bytes) == sha256.Size
}

// WriteAssetObject installs durable immutable bytes. Concurrent identical
// uploads converge on the same object, without replacing anything in use.
func WriteAssetObject(workspaceDir, digest string, body []byte) error {
	actual := sha256.Sum256(body)
	if !validDigest(digest) || hex.EncodeToString(actual[:]) != digest {
		return fmt.Errorf("invalid asset digest")
	}
	dir := filepath.Join(workspaceDir, ".objects")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(body); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	path := filepath.Join(dir, digest)
	if err = os.Link(file.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	stored, err := readBounded(path, int64(len(body)))
	if err != nil || sha256.Sum256(stored) != actual {
		return fmt.Errorf("asset object integrity failure")
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// ReadAssetObject supports legacy name-based payloads, but only when their
// bytes match the catalog/snapshot hash. A stale mutable file is never served
// under the identity of another image.
func ReadAssetObject(workspaceDir, name, digest string, limit int64) ([]byte, error) {
	if !sld.ValidStyleName(name) || !validDigest(digest) {
		return nil, fmt.Errorf("invalid managed asset identity")
	}
	if limit <= 0 {
		limit = 5 << 20
	}
	body, err := readBounded(filepath.Join(workspaceDir, ".objects", digest), limit)
	if errors.Is(err, os.ErrNotExist) {
		body, err = readBounded(filepath.Join(workspaceDir, name), limit)
	}
	if err != nil {
		return nil, err
	}
	actual := sha256.Sum256(body)
	if hex.EncodeToString(actual[:]) != digest {
		return nil, fmt.Errorf("managed asset hash mismatch")
	}
	return body, nil
}
