// Package links provides link validation utilities for OGC API tests.
package links

import (
	"strings"
	"testing"
)

// Link represents a link in OGC API responses.
type Link struct {
	Href  string `json:"href"`
	Rel   string `json:"rel"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// ParseLinks extracts links from a JSON response map.
func ParseLinks(json map[string]any) []Link {
	linksData, ok := json["links"].([]any)
	if !ok {
		return nil
	}

	var links []Link
	for _, l := range linksData {
		linkMap, ok := l.(map[string]any)
		if !ok {
			continue
		}
		links = append(links, Link{
			Href:  getString(linkMap, "href"),
			Rel:   getString(linkMap, "rel"),
			Type:  getString(linkMap, "type"),
			Title: getString(linkMap, "title"),
		})
	}
	return links
}

// FindByRel finds the first link with the given relation.
func FindByRel(links []Link, rel string) *Link {
	for i := range links {
		if links[i].Rel == rel {
			return &links[i]
		}
	}
	return nil
}

// FindAllByRel finds all links with the given relation.
func FindAllByRel(links []Link, rel string) []Link {
	var result []Link
	for _, l := range links {
		if l.Rel == rel {
			result = append(result, l)
		}
	}
	return result
}

// FindByRelAndType finds a link with the given relation and type.
func FindByRelAndType(links []Link, rel, mediaType string) *Link {
	for i := range links {
		if links[i].Rel == rel && strings.HasPrefix(links[i].Type, mediaType) {
			return &links[i]
		}
	}
	return nil
}

// ValidateSelfLink checks that a self link exists.
func ValidateSelfLink(t *testing.T, links []Link) {
	t.Helper()
	if FindByRel(links, "self") == nil {
		t.Error("Expected 'self' link to exist")
	}
}

// ValidateAlternateLinks checks that alternate links exist for media types.
func ValidateAlternateLinks(t *testing.T, links []Link, mediaTypes []string) {
	t.Helper()
	alternates := FindAllByRel(links, "alternate")

	for _, mt := range mediaTypes {
		found := false
		for _, l := range alternates {
			if strings.HasPrefix(l.Type, mt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected alternate link for media type %q", mt)
		}
	}
}

// ValidateLinksHaveRelAndType checks all links have rel and type.
func ValidateLinksHaveRelAndType(t *testing.T, links []Link, rels ...string) {
	t.Helper()

	relSet := make(map[string]bool)
	for _, r := range rels {
		relSet[r] = true
	}

	for i, l := range links {
		// Only check specified rels, or all if none specified
		if len(rels) > 0 && !relSet[l.Rel] {
			continue
		}

		if l.Rel == "" {
			t.Errorf("Link %d is missing 'rel' property", i)
		}
		if l.Type == "" {
			t.Errorf("Link %d (rel=%q) is missing 'type' property", i, l.Rel)
		}
	}
}

// ValidateLandingPageLinks validates required landing page links.
func ValidateLandingPageLinks(t *testing.T, links []Link) {
	t.Helper()

	// Must have service-desc or service-doc
	hasServiceDesc := FindByRel(links, "service-desc") != nil
	hasServiceDoc := FindByRel(links, "service-doc") != nil
	if !hasServiceDesc && !hasServiceDoc {
		t.Error("Landing page must include 'service-desc' or 'service-doc' link")
	}

	// Must have conformance link
	if FindByRel(links, "conformance") == nil {
		t.Error("Landing page must include 'conformance' link")
	}

	// Must have data link
	if FindByRel(links, "data") == nil {
		t.Error("Landing page must include 'data' link")
	}
}

// ValidateCollectionLinks validates required collection links.
func ValidateCollectionLinks(t *testing.T, links []Link) {
	t.Helper()

	// Must have items link
	if FindByRel(links, "items") == nil {
		t.Error("Collection must include 'items' link")
	}
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
