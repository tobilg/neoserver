package wms

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobilg/neoserver/internal/sld"
)

func safeStylePath(root, name string) (string, error) {
	if !sld.ValidStyleName(name) {
		return "", fmt.Errorf("invalid style name")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(rootReal, name+".sld")
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", fmt.Errorf("resolve style path: %w", err)
	}
	rel, err := filepath.Rel(rootReal, real)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return "", fmt.Errorf("style path escapes configured root")
	}
	return real, nil
}
