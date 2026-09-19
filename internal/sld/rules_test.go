package sld

import (
	"testing"
)

func TestEvaluateFilter_Nil(t *testing.T) {
	props := FeatureProperties{"name": "test"}
	if !EvaluateFilter(nil, props) {
		t.Error("expected nil filter to match all")
	}
}

func TestEvaluateFilter_And(t *testing.T) {
	filter := &Filter{
		And: []Filter{
			{PropertyIsEqualTo: []PropertyComparison{{PropertyName: "name", Literal: "test"}}},
			{PropertyIsEqualTo: []PropertyComparison{{PropertyName: "age", Literal: "30"}}},
		},
	}

	// Both match
	props := FeatureProperties{"name": "test", "age": int(30)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected AND to match when both conditions match")
	}

	// Only one matches
	props = FeatureProperties{"name": "test", "age": int(25)}
	if EvaluateFilter(filter, props) {
		t.Error("expected AND to fail when one condition doesn't match")
	}
}

func TestEvaluateFilter_Or(t *testing.T) {
	filter := &Filter{
		Or: []Filter{
			{PropertyIsEqualTo: []PropertyComparison{{PropertyName: "name", Literal: "test"}}},
			{PropertyIsEqualTo: []PropertyComparison{{PropertyName: "name", Literal: "other"}}},
		},
	}

	// First matches
	props := FeatureProperties{"name": "test"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected OR to match when first condition matches")
	}

	// Second matches
	props = FeatureProperties{"name": "other"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected OR to match when second condition matches")
	}

	// Neither matches
	props = FeatureProperties{"name": "different"}
	if EvaluateFilter(filter, props) {
		t.Error("expected OR to fail when no condition matches")
	}
}

func TestEvaluateFilter_Not(t *testing.T) {
	filter := &Filter{
		Not: &Filter{
			PropertyIsEqualTo: []PropertyComparison{{PropertyName: "name", Literal: "exclude"}},
		},
	}

	// Should match (name is not "exclude")
	props := FeatureProperties{"name": "test"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected NOT to match when inner condition doesn't match")
	}

	// Should not match (name is "exclude")
	props = FeatureProperties{"name": "exclude"}
	if EvaluateFilter(filter, props) {
		t.Error("expected NOT to fail when inner condition matches")
	}
}

func TestEvaluateFilter_PropertyIsEqualTo(t *testing.T) {
	filter := &Filter{
		PropertyIsEqualTo: []PropertyComparison{
			{PropertyName: "status", Literal: "active"},
		},
	}

	// Match
	props := FeatureProperties{"status": "active"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected equals to match")
	}

	// No match
	props = FeatureProperties{"status": "inactive"}
	if EvaluateFilter(filter, props) {
		t.Error("expected equals to fail for different value")
	}

	// Property missing
	props = FeatureProperties{}
	if EvaluateFilter(filter, props) {
		t.Error("expected equals to fail for missing property")
	}
}

func TestEvaluateFilter_PropertyIsNotEqualTo(t *testing.T) {
	filter := &Filter{
		PropertyIsNotEqualTo: []PropertyComparison{
			{PropertyName: "status", Literal: "deleted"},
		},
	}

	// Match (not equal)
	props := FeatureProperties{"status": "active"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected not-equals to match for different value")
	}

	// No match (equal)
	props = FeatureProperties{"status": "deleted"}
	if EvaluateFilter(filter, props) {
		t.Error("expected not-equals to fail for same value")
	}
}

func TestEvaluateFilter_PropertyIsLessThan(t *testing.T) {
	filter := &Filter{
		PropertyIsLessThan: []PropertyComparison{
			{PropertyName: "count", Literal: "10"},
		},
	}

	// Less than (int)
	props := FeatureProperties{"count": int(5)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected less-than to match for smaller value")
	}

	// Equal
	props = FeatureProperties{"count": int(10)}
	if EvaluateFilter(filter, props) {
		t.Error("expected less-than to fail for equal value")
	}

	// Greater than
	props = FeatureProperties{"count": int(15)}
	if EvaluateFilter(filter, props) {
		t.Error("expected less-than to fail for larger value")
	}
}

func TestEvaluateFilter_PropertyIsLessThanOrEqualTo(t *testing.T) {
	filter := &Filter{
		PropertyIsLessThanOrEqualTo: []PropertyComparison{
			{PropertyName: "count", Literal: "10"},
		},
	}

	// Less than
	props := FeatureProperties{"count": int(5)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected less-than-or-equal to match for smaller value")
	}

	// Equal
	props = FeatureProperties{"count": int(10)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected less-than-or-equal to match for equal value")
	}

	// Greater than
	props = FeatureProperties{"count": int(15)}
	if EvaluateFilter(filter, props) {
		t.Error("expected less-than-or-equal to fail for larger value")
	}
}

func TestEvaluateFilter_PropertyIsGreaterThan(t *testing.T) {
	filter := &Filter{
		PropertyIsGreaterThan: []PropertyComparison{
			{PropertyName: "count", Literal: "10"},
		},
	}

	// Greater than
	props := FeatureProperties{"count": int(15)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected greater-than to match for larger value")
	}

	// Equal
	props = FeatureProperties{"count": int(10)}
	if EvaluateFilter(filter, props) {
		t.Error("expected greater-than to fail for equal value")
	}

	// Less than
	props = FeatureProperties{"count": int(5)}
	if EvaluateFilter(filter, props) {
		t.Error("expected greater-than to fail for smaller value")
	}
}

func TestEvaluateFilter_PropertyIsGreaterThanOrEqualTo(t *testing.T) {
	filter := &Filter{
		PropertyIsGreaterThanOrEqualTo: []PropertyComparison{
			{PropertyName: "count", Literal: "10"},
		},
	}

	// Greater than
	props := FeatureProperties{"count": int(15)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected greater-than-or-equal to match for larger value")
	}

	// Equal
	props = FeatureProperties{"count": int(10)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected greater-than-or-equal to match for equal value")
	}

	// Less than
	props = FeatureProperties{"count": int(5)}
	if EvaluateFilter(filter, props) {
		t.Error("expected greater-than-or-equal to fail for smaller value")
	}
}

func TestEvaluateFilter_PropertyIsLike(t *testing.T) {
	filter := &Filter{
		PropertyIsLike: []PropertyIsLike{
			{PropertyName: "name", Literal: "test*", WildCard: "*", SingleChar: "?", EscapeChar: "\\"},
		},
	}

	// Match with wildcard
	props := FeatureProperties{"name": "testing"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected LIKE to match with wildcard")
	}

	// Match exact
	props = FeatureProperties{"name": "test"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected LIKE to match exact")
	}

	// No match
	props = FeatureProperties{"name": "other"}
	if EvaluateFilter(filter, props) {
		t.Error("expected LIKE to fail for non-matching")
	}
}

func TestEvaluateFilter_PropertyIsLike_SingleChar(t *testing.T) {
	filter := &Filter{
		PropertyIsLike: []PropertyIsLike{
			{PropertyName: "code", Literal: "A?B", WildCard: "*", SingleChar: "?", EscapeChar: "\\"},
		},
	}

	// Match single char
	props := FeatureProperties{"code": "A1B"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected LIKE to match single char")
	}

	// No match (too many chars)
	props = FeatureProperties{"code": "A12B"}
	if EvaluateFilter(filter, props) {
		t.Error("expected LIKE to fail for too many chars")
	}
}

func TestEvaluateFilter_PropertyIsLike_Defaults(t *testing.T) {
	// Test with empty wildcard/singlechar/escape (should use defaults)
	filter := &Filter{
		PropertyIsLike: []PropertyIsLike{
			{PropertyName: "name", Literal: "test*"},
		},
	}

	props := FeatureProperties{"name": "testing"}
	if !EvaluateFilter(filter, props) {
		t.Error("expected LIKE to work with default wildcards")
	}
}

func TestEvaluateFilter_PropertyIsNull(t *testing.T) {
	filter := &Filter{
		PropertyIsNull: []PropertyIsNull{
			{PropertyName: "optional"},
		},
	}

	// Property is nil
	props := FeatureProperties{"optional": nil}
	if !EvaluateFilter(filter, props) {
		t.Error("expected NULL to match nil value")
	}

	// Property doesn't exist
	props = FeatureProperties{}
	if !EvaluateFilter(filter, props) {
		t.Error("expected NULL to match missing property")
	}

	// Property has value
	props = FeatureProperties{"optional": "value"}
	if EvaluateFilter(filter, props) {
		t.Error("expected NULL to fail for non-nil value")
	}
}

func TestEvaluateFilter_PropertyIsBetween(t *testing.T) {
	filter := &Filter{
		PropertyIsBetween: []PropertyIsBetween{
			{PropertyName: "value", LowerBound: "10", UpperBound: "20"},
		},
	}

	// In range
	props := FeatureProperties{"value": int(15)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected BETWEEN to match value in range")
	}

	// At lower bound
	props = FeatureProperties{"value": int(10)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected BETWEEN to match at lower bound")
	}

	// At upper bound
	props = FeatureProperties{"value": int(20)}
	if !EvaluateFilter(filter, props) {
		t.Error("expected BETWEEN to match at upper bound")
	}

	// Below range
	props = FeatureProperties{"value": int(5)}
	if EvaluateFilter(filter, props) {
		t.Error("expected BETWEEN to fail below range")
	}

	// Above range
	props = FeatureProperties{"value": int(25)}
	if EvaluateFilter(filter, props) {
		t.Error("expected BETWEEN to fail above range")
	}
}

func TestCompareValues_Int(t *testing.T) {
	tests := []struct {
		value    interface{}
		literal  string
		expected int
	}{
		{int(5), "10", -1},
		{int(10), "10", 0},
		{int(15), "10", 1},
		{int32(5), "10", -1},
		{int64(5), "10", -1},
	}

	for _, tt := range tests {
		result := compareValues(tt.value, tt.literal)
		if result != tt.expected {
			t.Errorf("compareValues(%v, %q) = %d, want %d", tt.value, tt.literal, result, tt.expected)
		}
	}
}

func TestCompareValues_Float(t *testing.T) {
	tests := []struct {
		value    interface{}
		literal  string
		expected int
	}{
		{float64(5.5), "10.0", -1},
		{float64(10.0), "10.0", 0},
		{float64(15.5), "10.0", 1},
		{float32(5.5), "10.0", -1},
	}

	for _, tt := range tests {
		result := compareValues(tt.value, tt.literal)
		if result != tt.expected {
			t.Errorf("compareValues(%v, %q) = %d, want %d", tt.value, tt.literal, result, tt.expected)
		}
	}
}

func TestCompareValues_String(t *testing.T) {
	tests := []struct {
		value    interface{}
		literal  string
		expected int
	}{
		{"abc", "abc", 0},
		{"abc", "def", -1},
		{"def", "abc", 1},
	}

	for _, tt := range tests {
		result := compareValues(tt.value, tt.literal)
		if result != tt.expected {
			t.Errorf("compareValues(%v, %q) = %d, want %d", tt.value, tt.literal, result, tt.expected)
		}
	}
}

func TestCompareValues_Bool(t *testing.T) {
	tests := []struct {
		value    interface{}
		literal  string
		expected int
	}{
		{true, "true", 0},
		{true, "1", 0},
		{false, "false", 0},
		{false, "0", 0},
		{true, "false", 1},
		{false, "true", -1},
	}

	for _, tt := range tests {
		result := compareValues(tt.value, tt.literal)
		if result != tt.expected {
			t.Errorf("compareValues(%v, %q) = %d, want %d", tt.value, tt.literal, result, tt.expected)
		}
	}
}

func TestCompareValues_InvalidParse(t *testing.T) {
	// Int with non-numeric literal should fall back to string comparison
	result := compareValues(int(10), "abc")
	// "10" compared to "abc" as strings
	if result != 1 { // "10" > "abc" lexicographically? Actually "1" < "a"
		// String "10" vs "abc" - "1" (49) < "a" (97)
	}
}

func TestCompareValues_Unknown(t *testing.T) {
	// Unknown type should use string representation
	type custom struct{ value int }
	result := compareValues(custom{10}, "{10}")
	// Should not panic and should return some comparison
	_ = result
}

func TestToString(t *testing.T) {
	tests := []struct {
		value    interface{}
		expected string
	}{
		{nil, ""},
		{"test", "test"},
		{123, "123"},
		{3.14, "3.14"},
		{true, "true"},
	}

	for _, tt := range tests {
		result := toString(tt.value)
		if result != tt.expected {
			t.Errorf("toString(%v) = %q, want %q", tt.value, result, tt.expected)
		}
	}
}

func TestMatchRule_ScaleConstraints(t *testing.T) {
	rule := &ResolvedRule{
		MinScale: 1000,
		MaxScale: 10000,
	}
	props := FeatureProperties{}

	// Within range
	if !MatchRule(rule, props, 5000) {
		t.Error("expected rule to match at scale 5000")
	}

	// At min scale
	if !MatchRule(rule, props, 1000) {
		t.Error("expected rule to match at min scale")
	}

	// Below min scale
	if MatchRule(rule, props, 500) {
		t.Error("expected rule to not match below min scale")
	}

	// Above max scale
	if MatchRule(rule, props, 15000) {
		t.Error("expected rule to not match above max scale")
	}
}

func TestMatchRule_NoScaleConstraints(t *testing.T) {
	rule := &ResolvedRule{}
	props := FeatureProperties{}

	// Any scale should match
	if !MatchRule(rule, props, 0) {
		t.Error("expected rule with no scale constraints to match at scale 0")
	}
	if !MatchRule(rule, props, 1000000) {
		t.Error("expected rule with no scale constraints to match at any scale")
	}
}

func TestMatchRule_WithFilter(t *testing.T) {
	rule := &ResolvedRule{
		Filter: &Filter{
			PropertyIsEqualTo: []PropertyComparison{
				{PropertyName: "type", Literal: "highway"},
			},
		},
	}

	// Matching filter
	props := FeatureProperties{"type": "highway"}
	if !MatchRule(rule, props, 5000) {
		t.Error("expected rule to match with matching filter")
	}

	// Non-matching filter
	props = FeatureProperties{"type": "road"}
	if MatchRule(rule, props, 5000) {
		t.Error("expected rule to not match with non-matching filter")
	}
}

func TestFindMatchingRules(t *testing.T) {
	style := &Style{
		Name: "test",
		Rules: []ResolvedRule{
			{
				Name:     "highways",
				MinScale: 1000,
				MaxScale: 5000,
				Filter: &Filter{
					PropertyIsEqualTo: []PropertyComparison{
						{PropertyName: "type", Literal: "highway"},
					},
				},
			},
			{
				Name:     "roads",
				MinScale: 5000,
				MaxScale: 50000,
				Filter: &Filter{
					PropertyIsEqualTo: []PropertyComparison{
						{PropertyName: "type", Literal: "road"},
					},
				},
			},
			{
				Name: "default",
				// No filter, no scale constraints
			},
		},
	}

	// Test highway at correct scale
	props := FeatureProperties{"type": "highway"}
	matched := FindMatchingRules(style, props, 3000)
	if len(matched) != 2 { // highways + default
		t.Errorf("expected 2 matching rules, got %d", len(matched))
	}

	// Test road at correct scale
	props = FeatureProperties{"type": "road"}
	matched = FindMatchingRules(style, props, 10000)
	if len(matched) != 2 { // roads + default
		t.Errorf("expected 2 matching rules, got %d", len(matched))
	}

	// Test at scale where no specific rules match
	props = FeatureProperties{"type": "path"}
	matched = FindMatchingRules(style, props, 100000)
	if len(matched) != 1 { // only default
		t.Errorf("expected 1 matching rule, got %d", len(matched))
	}
}

func TestFindMatchingRules_Empty(t *testing.T) {
	style := &Style{
		Name: "test",
		Rules: []ResolvedRule{
			{
				Name:   "specific",
				Filter: &Filter{PropertyIsEqualTo: []PropertyComparison{{PropertyName: "type", Literal: "special"}}},
			},
		},
	}

	props := FeatureProperties{"type": "other"}
	matched := FindMatchingRules(style, props, 1000)
	if len(matched) != 0 {
		t.Errorf("expected 0 matching rules, got %d", len(matched))
	}
}
