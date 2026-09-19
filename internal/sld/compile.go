package sld

import (
	"errors"
	"fmt"
	"strings"
)

var ErrStyleMissing = errors.New("style is missing")

const (
	FormatSLD100 = "sld_1.0.0"
	FormatSLD110 = "sld_1.1.0"
	FormatSE110  = "se_1.1.0"
	FormatCSS    = "css"
	FormatYSLD   = "ysld"
	FormatMapbox = "mapbox"
)

// Diagnostic is a source-oriented style compilation message. The legacy
// management API continues to expose the flattened Message values as
// validation_errors while newer clients can use the structured fields.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message"`
}

// NormalizeFormat returns the canonical persisted format identifier. The
// historical sld_1.1.0 value is retained because existing catalogs use it.
func NormalizeFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return FormatSLD110, nil
	}
	switch value {
	case FormatSLD100, FormatSLD110, FormatSE110, FormatCSS, FormatYSLD, FormatMapbox:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported style format %q", value)
	}
}

func IsSLDFormat(value string) bool {
	value, err := NormalizeFormat(value)
	return err == nil && (value == FormatSLD100 || value == FormatSLD110 || value == FormatSE110)
}

// Compile parses and validates an authored style into the common document
// consumed by the renderers. Alternative format codecs are routed here as
// they are implemented so callers never need format-specific branches.
func Compile(format, body string) (*StyledLayerDescriptor, []Diagnostic, error) {
	canonical, err := NormalizeFormat(format)
	if err != nil {
		return nil, diagnosticsFor("unsupported-format", err), err
	}
	if strings.TrimSpace(body) == "" {
		err = fmt.Errorf("style body is required")
		return nil, diagnosticsFor("empty-style", err), err
	}

	switch canonical {
	case FormatSLD100, FormatSLD110, FormatSE110:
		doc, parseErr := ParseString(body)
		if parseErr != nil {
			return nil, diagnosticsFor("parse-error", parseErr), parseErr
		}
		if validateErr := Validate(doc); validateErr != nil {
			return nil, diagnosticsFor("validation-error", validateErr), validateErr
		}
		return doc, nil, nil
	case FormatCSS:
		doc, compileErr := compileCSS(body)
		return validateCompiledDocument(doc, compileErr)
	case FormatYSLD:
		doc, compileErr := compileYSLD(body)
		return validateCompiledDocument(doc, compileErr)
	case FormatMapbox:
		doc, compileErr := compileMapbox(body)
		return validateCompiledDocument(doc, compileErr)
	default:
		panic("unreachable style format")
	}
}

func validateCompiledDocument(doc *StyledLayerDescriptor, err error) (*StyledLayerDescriptor, []Diagnostic, error) {
	if err != nil {
		return nil, diagnosticsFor("compile-error", err), err
	}
	if err = Validate(doc); err != nil {
		return nil, diagnosticsFor("validation-error", err), err
	}
	return doc, nil, nil
}

func diagnosticsFor(code string, err error) []Diagnostic {
	if err == nil {
		return nil
	}
	diagnostic := Diagnostic{Severity: "error", Code: code, Message: err.Error()}
	var source *stylePathError
	if errors.As(err, &source) {
		diagnostic.Path = source.Path
	}
	var parse *ParseError
	if errors.As(err, &parse) {
		diagnostic.Line, diagnostic.Column = parse.Line, parse.Column
	}
	return []Diagnostic{diagnostic}
}

type stylePathError struct {
	Path string
	Err  error
}

func (e *stylePathError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *stylePathError) Unwrap() error { return e.Err }
