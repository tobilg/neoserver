package wfs

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/workspace"
)

// Versioned, self-contained map keys persist source identity without changing
// the catalog schema. The public name is presentation only, never lock identity.
type lockIdentity struct {
	Source string
	Name   string
}

func sourceLockKey(service *workspace.Service, layer *workspace.Layer, name string) string {
	source := service.ID + "/" + layer.SourceLayer
	if provider, ok := service.DataSource.(interface{ FeatureLockSource(string) string }); ok {
		source = provider.FeatureLockSource(layer.SourceLayer)
	}
	digest := sha256.Sum256([]byte(source))
	raw, _ := json.Marshal(lockIdentity{Source: fmt.Sprintf("%x", digest), Name: name})
	return "@source-v1/" + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLockKey(key string) (lockIdentity, bool) {
	if !strings.HasPrefix(key, "@source-v1/") {
		return lockIdentity{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, "@source-v1/"))
	var identity lockIdentity
	if err != nil || json.Unmarshal(raw, &identity) != nil || identity.Source == "" {
		return lockIdentity{}, false
	}
	return identity, true
}

func lockDisplayName(key string) string {
	if identity, ok := decodeLockKey(key); ok {
		key = identity.Name
	}
	return ParseQName(key).LocalPart
}
