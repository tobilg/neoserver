package sld

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FeatureProperties represents the properties of a feature for filter evaluation.
type FeatureProperties map[string]interface{}

// EvaluateFilter evaluates a filter against feature properties.
func EvaluateFilter(filter *Filter, props FeatureProperties) bool {
	if filter == nil {
		return true // No filter means all features match
	}

	// Handle logical operators
	if len(filter.And) > 0 {
		for _, f := range filter.And {
			if !EvaluateFilter(&f, props) {
				return false
			}
		}
		return true
	}

	if len(filter.Or) > 0 {
		for _, f := range filter.Or {
			if EvaluateFilter(&f, props) {
				return true
			}
		}
		return false
	}

	if filter.Not != nil {
		return !EvaluateFilter(filter.Not, props)
	}

	// Handle comparison operators
	for _, comp := range filter.PropertyIsEqualTo {
		if !evaluateEquals(comp, props) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsNotEqualTo {
		if evaluateEquals(comp, props) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsLessThan {
		if !evaluateLessThan(comp, props, false) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsLessThanOrEqualTo {
		if !evaluateLessThan(comp, props, true) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsGreaterThan {
		if !evaluateGreaterThan(comp, props, false) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsGreaterThanOrEqualTo {
		if !evaluateGreaterThan(comp, props, true) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsLike {
		if !evaluateLike(comp, props) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsNull {
		if !evaluateNull(comp, props) {
			return false
		}
	}

	for _, comp := range filter.PropertyIsBetween {
		if !evaluateBetween(comp, props) {
			return false
		}
	}

	return true
}

// evaluateEquals checks if a property equals a literal value.
func evaluateEquals(comp PropertyComparison, props FeatureProperties) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return false
	}

	return compareValues(propValue, comp.Literal) == 0
}

// evaluateLessThan checks if a property is less than a literal value.
func evaluateLessThan(comp PropertyComparison, props FeatureProperties, orEqual bool) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return false
	}

	cmp := compareValues(propValue, comp.Literal)
	if orEqual {
		return cmp <= 0
	}
	return cmp < 0
}

// evaluateGreaterThan checks if a property is greater than a literal value.
func evaluateGreaterThan(comp PropertyComparison, props FeatureProperties, orEqual bool) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return false
	}

	cmp := compareValues(propValue, comp.Literal)
	if orEqual {
		return cmp >= 0
	}
	return cmp > 0
}

// evaluateLike checks if a property matches a LIKE pattern.
func evaluateLike(comp PropertyIsLike, props FeatureProperties) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return false
	}

	strValue := toString(propValue)

	// Use pre-compiled regex if available (much faster for repeated evaluations)
	if comp.CompiledRegex != nil {
		return comp.CompiledRegex.MatchString(strValue)
	}

	// Fallback: compile regex on-the-fly (for dynamically created filters)
	wildCard := comp.WildCard
	if wildCard == "" {
		wildCard = "*"
	}
	singleChar := comp.SingleChar
	if singleChar == "" {
		singleChar = "?"
	}

	// Escape regex special characters
	regexPattern := regexp.QuoteMeta(comp.Literal)

	// Replace wildcard with .*
	regexPattern = strings.ReplaceAll(regexPattern, regexp.QuoteMeta(wildCard), ".*")

	// Replace single char with .
	regexPattern = strings.ReplaceAll(regexPattern, regexp.QuoteMeta(singleChar), ".")

	// Anchor the pattern
	regexPattern = "^" + regexPattern + "$"

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}

	return re.MatchString(strValue)
}

// evaluateNull checks if a property is null.
func evaluateNull(comp PropertyIsNull, props FeatureProperties) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return true // Property doesn't exist, treat as null
	}
	return propValue == nil
}

// evaluateBetween checks if a property is between two values.
func evaluateBetween(comp PropertyIsBetween, props FeatureProperties) bool {
	propValue, ok := props[comp.PropertyName]
	if !ok {
		return false
	}

	cmpLower := compareValues(propValue, comp.LowerBound)
	cmpUpper := compareValues(propValue, comp.UpperBound)

	return cmpLower >= 0 && cmpUpper <= 0
}

// compareValues compares a property value with a literal string.
// Returns -1 if propValue < literal, 0 if equal, 1 if propValue > literal.
func compareValues(propValue interface{}, literal string) int {
	switch v := propValue.(type) {
	case int:
		litVal, err := strconv.ParseInt(literal, 10, 64)
		if err != nil {
			return strings.Compare(toString(propValue), literal)
		}
		return compareInt(int64(v), litVal)
	case int32:
		litVal, err := strconv.ParseInt(literal, 10, 64)
		if err != nil {
			return strings.Compare(toString(propValue), literal)
		}
		return compareInt(int64(v), litVal)
	case int64:
		litVal, err := strconv.ParseInt(literal, 10, 64)
		if err != nil {
			return strings.Compare(toString(propValue), literal)
		}
		return compareInt(v, litVal)
	case float32:
		litVal, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			return strings.Compare(toString(propValue), literal)
		}
		return compareFloat(float64(v), litVal)
	case float64:
		litVal, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			return strings.Compare(toString(propValue), literal)
		}
		return compareFloat(v, litVal)
	case string:
		return strings.Compare(v, literal)
	case bool:
		litBool := strings.ToLower(literal) == "true" || literal == "1"
		if v == litBool {
			return 0
		}
		if v {
			return 1
		}
		return -1
	default:
		return strings.Compare(toString(propValue), literal)
	}
}

// compareInt compares two int64 values.
func compareInt(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareFloat compares two float64 values.
func compareFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// toString converts a value to its string representation.
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// MatchRule checks if a rule applies to a feature at a given scale.
func MatchRule(rule *ResolvedRule, props FeatureProperties, scale float64) bool {
	// Check scale constraints
	if rule.MinScale > 0 && scale < rule.MinScale {
		return false
	}
	if rule.MaxScale > 0 && scale > rule.MaxScale {
		return false
	}

	// Check filter
	return EvaluateFilter(rule.Filter, props)
}

// FindMatchingRules returns all rules that match a feature at a given scale.
func FindMatchingRules(style *Style, props FeatureProperties, scale float64) []ResolvedRule {
	var matched []ResolvedRule
	matchedRegular := false
	group := -1
	for _, rule := range style.Rules {
		if rule.FeatureTypeStyle != group {
			group = rule.FeatureTypeStyle
			matchedRegular = false
		}
		if rule.ElseFilter {
			if !matchedRegular && scaleMatches(&rule, scale) {
				matched = append(matched, rule)
			}
			continue
		}
		if MatchRule(&rule, props, scale) {
			matched = append(matched, rule)
			matchedRegular = true
		}
	}
	return matched
}

func scaleMatches(rule *ResolvedRule, scale float64) bool {
	if rule.MinScale > 0 && scale < rule.MinScale {
		return false
	}
	if rule.MaxScale > 0 && scale > rule.MaxScale {
		return false
	}
	return true
}
