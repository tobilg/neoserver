package cataloglifecycle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// AssetStore isolates managed style asset state so destructive lifecycle
// phases can be failure-tested without production failpoints.
type AssetStore interface {
	Count(workspaceID string) (int64, error)
	Stage(operationID, workspaceID string) error
	RemoveStaged(operationID string) error
}

type filesystemAssetStore struct{ root string }

func NewFilesystemAssetStore(root string) AssetStore { return &filesystemAssetStore{root: root} }

func (s *filesystemAssetStore) paths(operationID, workspaceID string) (source, staged string, err error) {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", "", err
	}
	if strings.ContainsAny(workspaceID, `/\\`) || strings.ContainsAny(operationID, `/\\`) {
		return "", "", errors.New("invalid lifecycle path identifier")
	}
	return filepath.Join(root, workspaceID), filepath.Join(root, ".deleting", operationID), nil
}

func (s *filesystemAssetStore) Count(workspaceID string) (int64, error) {
	path, _, err := s.paths("count", workspaceID)
	if err != nil {
		return 0, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("managed asset workspace path is not a directory")
	}
	count := int64(1)
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current != path {
			count++
		}
		return nil
	})
	return count, err
}

func (s *filesystemAssetStore) Stage(operationID, workspaceID string) error {
	source, staged, err := s.paths(operationID, workspaceID)
	if err != nil {
		return err
	}
	sourceInfo, sourceErr := os.Lstat(source)
	_, stagedErr := os.Lstat(staged)
	if errors.Is(sourceErr, os.ErrNotExist) && stagedErr == nil {
		return nil
	}
	if errors.Is(sourceErr, os.ErrNotExist) && errors.Is(stagedErr, os.ErrNotExist) {
		return nil
	}
	if sourceErr != nil {
		return sourceErr
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		return errors.New("managed asset workspace path is not a directory")
	}
	if stagedErr == nil {
		return errors.New("both active and staged managed asset paths exist")
	}
	if !errors.Is(stagedErr, os.ErrNotExist) {
		return stagedErr
	}
	if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
		return err
	}
	return os.Rename(source, staged)
}

func (s *filesystemAssetStore) RemoveStaged(operationID string) error {
	_, staged, err := s.paths(operationID, "unused")
	if err != nil {
		return err
	}
	return os.RemoveAll(staged)
}
