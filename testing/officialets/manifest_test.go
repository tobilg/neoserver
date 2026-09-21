package officialets

import (
	"os"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/conformance"
	"github.com/tobilg/neoserver/testing/officialets/manifest"
)

func TestOfficialSuitePinsAndClaimMappingsStayAligned(t *testing.T) {
	document, err := manifest.Load("versions.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Suites) != 6 {
		t.Fatalf("official suite manifest has %d suites, want 6", len(document.Suites))
	}

	compose, err := os.ReadFile("../../docker-compose.conformance.yml")
	if err != nil {
		t.Fatal(err)
	}
	derivedRunner, err := os.ReadFile("../../scripts/conformance/run-derived.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, derivedImageRef := range []string{
		"neoserver/ets-wcs20-interpolation-derived:wcs20-1.21-uri-xpath-v1",
		"neoserver/ets-wfs202-derived:wfs20-1.42-locking-v2",
	} {
		if !strings.Contains(string(compose), derivedImageRef) || !strings.Contains(string(derivedRunner), derivedImageRef) {
			t.Fatalf("official-derived image %s is not synchronized between Compose and its runner", derivedImageRef)
		}
	}
	for name, suite := range document.Suites {
		if !strings.Contains(suite.Image, "@sha256:") || len(strings.Split(suite.Image, "@sha256:")[1]) != 64 {
			t.Errorf("suite %s image is not digest-pinned: %q", name, suite.Image)
		}
		if !strings.Contains(string(compose), suite.Image) {
			t.Errorf("suite %s pin is not synchronized with Compose", name)
		}
	}

	for name, profile := range document.Profiles {
		if _, ok := document.Suites[profile.Suite]; !ok {
			t.Errorf("profile %s maps to unknown suite %q", name, profile.Suite)
		}
		switch profile.EvidenceKind {
		case "official":
			if profile.Patch != "" || profile.PatchSet != "" || profile.PatchSHA256 != "" {
				t.Errorf("stock profile %s declares a compatibility patch", name)
			}
		case "official-derived":
			if profile.Patch == "" || profile.PatchSet == "" || profile.PatchSHA256 == "" {
				t.Errorf("derived profile %s has incomplete patch identity", name)
				continue
			}
			actual, err := manifest.FileSHA256("../../" + profile.Patch)
			if err != nil {
				t.Errorf("read patch for %s: %v", name, err)
			} else if actual != profile.PatchSHA256 {
				t.Errorf("profile %s patch digest=%s, want %s", name, actual, profile.PatchSHA256)
			}
			dockerfile := map[string]string{
				"wcs20/interpolation": "patches/wcs20-interpolation.Dockerfile",
				"wfs20/core202":       "patches/wfs202-locking.Dockerfile",
			}[name]
			derivedDockerfile, err := os.ReadFile(dockerfile)
			if err != nil {
				t.Fatalf("read derived Dockerfile for %s: %v", name, err)
			}
			if !strings.Contains(string(compose), profile.PatchSet) || !strings.Contains(string(derivedDockerfile), profile.PatchSet) || !strings.Contains(string(derivedDockerfile), profile.Patch) || !strings.Contains(string(derivedDockerfile), document.Suites[profile.Suite].Image) {
				t.Errorf("derived profile %s is not synchronized with Compose and its Dockerfile", name)
			}
		default:
			t.Errorf("profile %s has unknown evidence kind %q", name, profile.EvidenceKind)
		}
	}

	for _, class := range conformance.All() {
		if class.OfficialProfile != "" {
			profile, ok := document.Profiles[class.OfficialProfile]
			if !ok || profile.EvidenceKind != "official" {
				t.Errorf("class %s maps to unavailable stock profile %q", class.Key, class.OfficialProfile)
			}
		}
		if class.DerivedProfile != "" {
			profile, ok := document.Profiles[class.DerivedProfile]
			if !ok || profile.EvidenceKind != "official-derived" {
				t.Errorf("class %s maps to unavailable derived profile %q", class.Key, class.DerivedProfile)
			}
		}
	}
}
