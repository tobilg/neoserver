package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Suite struct {
	SuiteCode string `json:"suite_code"`
	Image     string `json:"image"`
}

type Profile struct {
	Suite        string            `json:"suite"`
	EvidenceKind string            `json:"evidence_kind"`
	Arguments    map[string]string `json:"arguments"`
	PatchSet     string            `json:"patch_set,omitempty"`
	Patch        string            `json:"patch,omitempty"`
	PatchSHA256  string            `json:"patch_sha256,omitempty"`
}

type Document struct {
	SchemaVersion int                `json:"schema_version"`
	TeamEngineAPI string             `json:"teamengine_api"`
	Suites        map[string]Suite   `json:"suites"`
	Profiles      map[string]Profile `json:"profiles"`
}

func Load(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, err
	}
	if document.SchemaVersion != 2 {
		return Document{}, fmt.Errorf("unsupported official ETS manifest schema %d", document.SchemaVersion)
	}
	return document, nil
}

func (d Document) ProfileNames(kind, suite string) []string {
	names := make([]string, 0)
	for name, profile := range d.Profiles {
		if profile.EvidenceKind == kind && profile.Suite == suite {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (d Document) Arguments(name, server string) ([]byte, error) {
	profile, ok := d.Profiles[name]
	if !ok {
		return nil, fmt.Errorf("unknown ETS profile %q", name)
	}
	values := make(map[string]string, len(profile.Arguments))
	for key, value := range profile.Arguments {
		values[key] = strings.ReplaceAll(value, "${SERVER}", strings.TrimRight(server, "/"))
	}
	return json.Marshal(values)
}

func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
