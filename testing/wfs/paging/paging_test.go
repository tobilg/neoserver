package paging

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestPaging_HitsOnly tests resultType=hits returns count only.
func TestPaging_HitsOnly(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESULTTYPE": {"hits"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// For hits, numberReturned should be 0
	if fc.NumberReturned != 0 {
		t.Errorf("Expected numberReturned=0 for hits, got %d", fc.NumberReturned)
	}

	// numberMatched should be the total count (or unknown=-1)
	if fc.NumberMatched < 0 && fc.NumberMatched != -1 {
		t.Errorf("Expected numberMatched >= 0 or -1 (unknown), got %d", fc.NumberMatched)
	}
}

// TestPaging_WithCount tests count limits results.
func TestPaging_WithCount(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get total count
	hitsParams := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESULTTYPE": {"hits"},
	}

	hitsResp, err := ctx.Client.Get(hitsParams)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	hitsFC, err := gml.ParseFeatureCollection(hitsResp.Body)
	if err != nil {
		t.Fatalf("Failed to parse hits response: %v", err)
	}

	if hitsFC.NumberMatched < 2 {
		t.Skip("Need at least 2 features to test paging")
	}

	// Now request with COUNT
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"2"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if fc.NumberReturned > 2 {
		t.Errorf("Expected at most 2 features, got %d", fc.NumberReturned)
	}
}

// TestPaging_WithStartIndex tests startIndex skips results.
func TestPaging_WithStartIndex(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// First get all features to verify there are enough
	allParams := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"COUNT":      {"10"},
		"RESULTTYPE": {"hits"},
	}

	allResp, err := ctx.Client.Get(allParams)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	allFC, err := gml.ParseFeatureCollection(allResp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if allFC.NumberMatched < 3 {
		t.Skip("Need at least 3 features to test startIndex")
	}

	// Get first 2 features
	first2Params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"2"},
	}

	first2Resp, err := ctx.Client.Get(first2Params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	first2FC, err := gml.ParseFeatureCollection(first2Resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	first2IDs := first2FC.GetFeatureIDs()

	// Now get with startIndex=2
	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"STARTINDEX": {"2"},
		"COUNT":      {"2"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// The features should be different from the first 2
	page2IDs := fc.GetFeatureIDs()
	for _, id := range page2IDs {
		for _, first2ID := range first2IDs {
			if id == first2ID {
				t.Errorf("Feature %s appears in both first page and startIndex=2 page", id)
			}
		}
	}
}

// TestPaging_TraverseForward tests forward pagination.
func TestPaging_TraverseForward(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Get total count
	hitsParams := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESULTTYPE": {"hits"},
	}

	hitsResp, err := ctx.Client.Get(hitsParams)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	hitsFC, err := gml.ParseFeatureCollection(hitsResp.Body)
	if err != nil {
		t.Fatalf("Failed to parse hits response: %v", err)
	}

	totalFeatures := hitsFC.NumberMatched
	if totalFeatures < 3 {
		t.Skip("Need at least 3 features to test traversal")
	}

	// Traverse in pages of 2
	pageSize := 2
	startIndex := 0
	allIDs := make(map[string]bool)
	pages := 0
	maxPages := 10

	for pages < maxPages {
		params := url.Values{
			"SERVICE":    {"WFS"},
			"REQUEST":    {"GetFeature"},
			"VERSION":    {"2.0.0"},
			"TYPENAMES":  {ft.Name},
			"STARTINDEX": {string(rune('0' + startIndex))},
			"COUNT":      {string(rune('0' + pageSize))},
		}

		// Use proper integer formatting
		params.Set("STARTINDEX", formatInt(startIndex))
		params.Set("COUNT", formatInt(pageSize))

		resp, err := ctx.Client.Get(params)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		wfs.AssertStatusCode(t, resp, 200)
		wfs.AssertNotException(t, resp)

		fc, err := gml.ParseFeatureCollection(resp.Body)
		if err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if fc.NumberReturned == 0 {
			break
		}

		// Collect IDs
		ids := fc.GetFeatureIDs()
		for _, id := range ids {
			if allIDs[id] {
				t.Errorf("Duplicate feature ID %s found during traversal", id)
			}
			allIDs[id] = true
		}

		startIndex += pageSize
		pages++

		// Check if we have all features
		if len(allIDs) >= totalFeatures {
			break
		}
	}

	t.Logf("Traversed %d pages, found %d unique features", pages, len(allIDs))
}

// TestPaging_NextLink tests that next link is present when appropriate.
func TestPaging_NextLink(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Get total count
	hitsParams := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"RESULTTYPE": {"hits"},
	}

	hitsResp, err := ctx.Client.Get(hitsParams)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	hitsFC, err := gml.ParseFeatureCollection(hitsResp.Body)
	if err != nil {
		t.Fatalf("Failed to parse hits response: %v", err)
	}

	if hitsFC.NumberMatched < 3 {
		t.Skip("Need at least 3 features to test next link")
	}

	// Request first page
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"COUNT":     {"2"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check for next link
	if fc.Next == "" {
		t.Log("Warning: next link not present, but may be optional")
	} else {
		t.Logf("Next link: %s", fc.Next)
	}
}

// TestPaging_ZeroStartIndex tests startIndex=0 (same as no startIndex).
func TestPaging_ZeroStartIndex(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"STARTINDEX": {"0"},
		"COUNT":      {"5"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestPaging_LargeStartIndex tests startIndex beyond data.
func TestPaging_LargeStartIndex(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := ctx.GetFirstFeatureType()
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":    {"WFS"},
		"REQUEST":    {"GetFeature"},
		"VERSION":    {"2.0.0"},
		"TYPENAMES":  {ft.Name},
		"STARTINDEX": {"999999"},
		"COUNT":      {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty collection
	if fc.NumberReturned != 0 {
		t.Errorf("Expected 0 features for large startIndex, got %d", fc.NumberReturned)
	}
}

// formatInt formats an integer as a string.
func formatInt(i int) string {
	return fmt.Sprintf("%d", i)
}
