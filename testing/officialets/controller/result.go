package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

type resultCounts struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type caseResult struct {
	Name         string
	Class        string
	Kind         string
	Status       string
	Message      string
	SkipCategory string
}

type suiteResult struct {
	Format         string         `json:"format"`
	Total          int            `json:"total"`
	Passed         int            `json:"passed"`
	Failed         int            `json:"failed"`
	Skipped        int            `json:"skipped"`
	Leaf           resultCounts   `json:"leaf"`
	Wrapper        resultCounts   `json:"wrapper"`
	Infrastructure resultCounts   `json:"infrastructure"`
	SkipCategories map[string]int `json:"skip_categories,omitempty"`
	Message        string         `json:"message,omitempty"`
	Cases          []caseResult   `json:"-"`
}

func parseSuiteResult(document []byte) (suiteResult, error) {
	decoder := xml.NewDecoder(bytes.NewReader(document))
	for {
		token, err := decoder.Token()
		if err != nil {
			return suiteResult{}, fmt.Errorf("parse TEAM Engine result: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		var result suiteResult
		var parseErr error
		switch strings.ToLower(start.Name.Local) {
		case "testng-results":
			result, parseErr = parseTestNGResult(document)
		case "execution":
			result, parseErr = parseCTLResult(decoder, start)
		default:
			return suiteResult{}, fmt.Errorf("unsupported TEAM Engine result root %q", start.Name.Local)
		}
		classifySkippedCases(&result)
		return result, parseErr
	}
}

// classifySkippedCases keeps skipped assertions visible while making their
// cause machine-readable. This separates a deliberately unclaimed optional
// branch from a fixture gap or a controller/setup problem.
func classifySkippedCases(result *suiteResult) {
	result.SkipCategories = make(map[string]int)
	// TestNG skips dependent setup/cleanup methods without repeating the
	// capability check that disabled their test group.
	parents := make(map[string]string)
	for _, item := range result.Cases {
		if item.Status == "skipped" && strings.Contains(item.Message, "Capability not implemented:") {
			if group := optionalTestGroup(item.Class); group != "" {
				parents[group] = strings.Split(item.Message, "\n")[0]
			}
		}
	}
	for index := range result.Cases {
		item := &result.Cases[index]
		if item.Status != "skipped" {
			continue
		}
		if item.Kind == "infrastructure" && strings.TrimSpace(item.Message) == "" {
			if cause := parents[optionalTestGroup(item.Class)]; cause != "" {
				item.Message = "Dependent setup/cleanup skipped: " + cause
			}
		}
		item.SkipCategory = skipCategory(*item)
		result.SkipCategories[item.SkipCategory]++
	}
	if len(result.SkipCategories) == 0 {
		result.SkipCategories = nil
	}
}

func optionalTestGroup(class string) string {
	for _, group := range []string{".joins.", ".versioning."} {
		if strings.Contains(class, group) {
			return group
		}
	}
	return ""
}

func skipCategory(item caseResult) string {
	value := strings.ToLower(strings.Join([]string{item.Name, item.Class, item.Message}, " "))
	if item.Kind == "infrastructure" {
		return "infrastructure"
	}
	if strings.Contains(value, "2.0.2") || strings.Contains(value, "profile version") {
		return "profile-version"
	}
	if containsAny(value, "no numeric", "no temporal", "no nillable", "nillable property", "no feature", "no property", "no value", "fixture", "not applicable", "geometry operand", "not supported for point geometry types") {
		return "fixture-not-applicable"
	}
	if containsAny(value, "dimension", "matrix limit", "matrixlimit", "tilematrixsetlimit", "gettile.optional", "legendurl", "wellknownscaleset", "well known scale set", "updatesequence", "acceptversions", "acceptformats") {
		return "conditional-protocol-branch"
	}
	if containsAny(value, ".joins.", ".versioning.", "not supported", "does not support", "not implemented", "not advertised", "unadvertised", "not claimed") {
		return "unclaimed-optional-capability"
	}
	return "unclassified"
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

type testNGDocument struct {
	Total   int `xml:"total,attr"`
	Failed  int `xml:"failed,attr"`
	Skipped int `xml:"skipped,attr"`
	Suites  []struct {
		Tests []struct {
			Name    string `xml:"name,attr"`
			Classes []struct {
				Name    string `xml:"name,attr"`
				Methods []struct {
					Name        string `xml:"name,attr"`
					Status      string `xml:"status,attr"`
					Description string `xml:"description,attr"`
					Signature   string `xml:"signature,attr"`
					IsConfig    string `xml:"is-config,attr"`
					Exception   *struct {
						Message string `xml:"message"`
						Stack   string `xml:"full-stacktrace"`
					} `xml:"exception"`
				} `xml:"test-method"`
			} `xml:"class"`
		} `xml:"test"`
	} `xml:"suite"`
}

func parseTestNGResult(document []byte) (suiteResult, error) {
	var parsed testNGDocument
	if err := xml.Unmarshal(document, &parsed); err != nil {
		return suiteResult{}, fmt.Errorf("parse TestNG result: %w", err)
	}
	result := suiteResult{
		Format: "testng", Total: parsed.Total, Failed: parsed.Failed, Skipped: parsed.Skipped,
	}
	result.Passed = result.Total - result.Failed - result.Skipped
	for _, suite := range parsed.Suites {
		for _, test := range suite.Tests {
			for _, class := range test.Classes {
				for _, method := range class.Methods {
					kind := "assertion"
					counts := &result.Leaf
					if strings.EqualFold(method.IsConfig, "true") {
						kind = "infrastructure"
						counts = &result.Infrastructure
					}
					status, statusErr := normalizedStatus(method.Status)
					message := strings.TrimSpace(method.Description)
					if method.Exception != nil {
						message = strings.TrimSpace(strings.Join([]string{method.Exception.Message, method.Exception.Stack}, "\n"))
					}
					if statusErr != nil {
						status = "failed"
						message = statusErr.Error()
					}
					addCount(counts, status)
					name := method.Name
					if name == "" {
						name = method.Signature
					}
					result.Cases = append(result.Cases, caseResult{Name: name, Class: class.Name, Kind: kind, Status: status, Message: message})
				}
			}
		}
	}
	if result.Total == 0 {
		return result, fmt.Errorf("TEAM Engine executed no tests")
	}
	if result.Failed > 0 || result.Leaf.Failed > 0 || result.Infrastructure.Failed > 0 {
		return result, fmt.Errorf("TEAM Engine reported %d failed test(s)", result.Failed)
	}
	if result.Passed <= 0 {
		return result, fmt.Errorf("TEAM Engine reported no passed tests")
	}
	return result, nil
}

type ctlFrame struct {
	Name     string
	Class    string
	Path     string
	HasChild bool
	Message  string
}

type ctlOutcome struct {
	Frame     ctlFrame
	RawStatus string
}

func parseCTLResult(decoder *xml.Decoder, root xml.StartElement) (suiteResult, error) {
	result := suiteResult{Format: "ctl"}
	stack := make([]ctlFrame, 0)
	outcomes := make([]ctlOutcome, 0)
	for {
		token, err := decoder.Token()
		if err != nil {
			return result, fmt.Errorf("parse CTL execution result: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch {
			case strings.EqualFold(value.Name.Local, "message"):
				message, err := decodeText(decoder)
				if err != nil {
					return result, fmt.Errorf("parse CTL message: %w", err)
				}
				if len(stack) > 0 && message != "" {
					frame := &stack[len(stack)-1]
					frame.Message = strings.TrimSpace(frame.Message + "\n" + message)
				}
			case strings.EqualFold(value.Name.Local, "starttest"):
				if len(stack) > 0 {
					stack[len(stack)-1].HasChild = true
				}
				name := attribute(value, "local-name")
				if name == "" {
					name = attribute(value, "path")
				}
				stack = append(stack, ctlFrame{Name: name, Class: attribute(value, "file"), Path: attribute(value, "path")})
			case strings.EqualFold(value.Name.Local, "endtest"):
				if len(stack) == 0 {
					return result, fmt.Errorf("TEAM Engine CTL result contains unmatched endtest")
				}
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				outcomes = append(outcomes, ctlOutcome{Frame: frame, RawStatus: strings.ToLower(strings.TrimSpace(attribute(value, "result")))})
			}
		case xml.EndElement:
			if value.Name == root.Name {
				if len(stack) != 0 {
					return result, fmt.Errorf("TEAM Engine CTL execution ended with %d open test(s)", len(stack))
				}
				unknown := classifyCTLOutcomes(&result, outcomes)
				result.Total, result.Passed, result.Failed, result.Skipped = result.Leaf.Total, result.Leaf.Passed, result.Leaf.Failed, result.Leaf.Skipped
				if result.Total == 0 && result.Infrastructure.Total == 0 {
					return result, fmt.Errorf("TEAM Engine CTL execution contained no leaf test results")
				}
				if len(unknown) > 0 {
					return result, fmt.Errorf("%s", strings.Join(unknown, "; "))
				}
				if result.Failed > 0 || result.Wrapper.Failed > 0 || result.Infrastructure.Failed > 0 {
					return result, fmt.Errorf("TEAM Engine reported %d failed leaf test(s) and %d failed wrapper test(s)", result.Failed, result.Wrapper.Failed)
				}
				if result.Passed == 0 {
					return result, fmt.Errorf("TEAM Engine reported no passed leaf tests")
				}
				return result, nil
			}
		}
	}
}

// CTL result logs represent calls by path references rather than XML nesting.
// A parent starttest/endtest pair can therefore be a sibling of all the tests
// it invokes. Direct nesting still appears in synthetic/minimal documents, so
// both signals participate in wrapper classification.
func classifyCTLOutcomes(result *suiteResult, outcomes []ctlOutcome) []string {
	var unknown []string
	for _, outcome := range outcomes {
		status, statusErr := normalizedStatus(outcome.RawStatus)
		if statusErr != nil {
			message := fmt.Sprintf("unknown CTL test result %q", outcome.RawStatus)
			addCount(&result.Infrastructure, "failed")
			result.Cases = append(result.Cases, caseResult{Name: outcome.Frame.Name, Class: outcome.Frame.Class, Kind: "infrastructure", Status: "failed", Message: message})
			unknown = append(unknown, message)
			continue
		}

		hasChild := outcome.Frame.HasChild
		if outcome.Frame.Path != "" {
			prefix := strings.TrimSuffix(outcome.Frame.Path, "/") + "/"
			for _, candidate := range outcomes {
				if candidate.Frame.Path != outcome.Frame.Path && strings.HasPrefix(candidate.Frame.Path, prefix) {
					hasChild = true
					break
				}
			}
		}
		if hasChild {
			addCount(&result.Wrapper, status)
			continue
		}
		addCount(&result.Leaf, status)
		result.Cases = append(result.Cases, caseResult{Name: outcome.Frame.Name, Class: outcome.Frame.Class, Kind: "assertion", Status: status, Message: outcome.Frame.Message})
	}
	return unknown
}

// decodeText consumes the current element, preserving text in nested markup.
func decodeText(decoder *xml.Decoder) (string, error) {
	var body strings.Builder
	for depth := 1; depth > 0; {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			body.Write(value)
		}
	}
	return strings.TrimSpace(body.String()), nil
}

func normalizedStatus(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "pass", "passed", "success", "bestpractice", "best practice":
		return "passed", nil
	case "2", "3", "4", "skip", "skipped", "not tested", "nottested", "warning":
		return "skipped", nil
	case "5", "6", "fail", "failed", "failure", "inheritedfailure", "inherited failure":
		return "failed", nil
	default:
		return "", fmt.Errorf("unknown test result %q", value)
	}
}

func addCount(counts *resultCounts, status string) {
	counts.Total++
	switch status {
	case "passed":
		counts.Passed++
	case "failed":
		counts.Failed++
	case "skipped":
		counts.Skipped++
	}
}

func attribute(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}
