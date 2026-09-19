package sld

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSLD(t *testing.T) {
	sldXML := `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor version="1.1.0">
  <NamedLayer>
    <Name>testlayer</Name>
    <UserStyle>
      <Name>teststyle</Name>
      <Title>Test Style</Title>
      <IsDefault>true</IsDefault>
      <FeatureTypeStyle>
        <Rule>
          <Name>rule1</Name>
          <Title>Rule 1</Title>
          <PolygonSymbolizer>
            <Fill>
              <CssParameter name="fill">#ff0000</CssParameter>
              <CssParameter name="fill-opacity">0.5</CssParameter>
            </Fill>
            <Stroke>
              <CssParameter name="stroke">#000000</CssParameter>
              <CssParameter name="stroke-width">2</CssParameter>
            </Stroke>
          </PolygonSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>`

	sld, err := Parse(strings.NewReader(sldXML))
	if err != nil {
		t.Fatalf("Failed to parse SLD: %v", err)
	}

	if len(sld.NamedLayers) != 1 {
		t.Errorf("Expected 1 NamedLayer, got %d", len(sld.NamedLayers))
	}

	nl := sld.NamedLayers[0]
	if nl.Name != "testlayer" {
		t.Errorf("Expected layer name 'testlayer', got '%s'", nl.Name)
	}

	if len(nl.UserStyles) != 1 {
		t.Errorf("Expected 1 UserStyle, got %d", len(nl.UserStyles))
	}

	us := nl.UserStyles[0]
	if us.Name != "teststyle" {
		t.Errorf("Expected style name 'teststyle', got '%s'", us.Name)
	}

	if !us.IsDefault {
		t.Error("Expected IsDefault to be true")
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		input    string
		expected struct{ r, g, b uint8 }
	}{
		{"#ff0000", struct{ r, g, b uint8 }{255, 0, 0}},
		{"#00ff00", struct{ r, g, b uint8 }{0, 255, 0}},
		{"#0000ff", struct{ r, g, b uint8 }{0, 0, 255}},
		{"#fff", struct{ r, g, b uint8 }{255, 255, 255}},
		{"red", struct{ r, g, b uint8 }{255, 0, 0}},
		{"blue", struct{ r, g, b uint8 }{0, 0, 255}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c := parseColor(tt.input)
			if c.R != tt.expected.r || c.G != tt.expected.g || c.B != tt.expected.b {
				t.Errorf("parseColor(%s) = RGB(%d,%d,%d), want RGB(%d,%d,%d)",
					tt.input, c.R, c.G, c.B, tt.expected.r, tt.expected.g, tt.expected.b)
			}
		})
	}
}

func TestGetStyle(t *testing.T) {
	sldXML := `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor version="1.1.0">
  <NamedLayer>
    <Name>testlayer</Name>
    <UserStyle>
      <Name>default</Name>
      <IsDefault>true</IsDefault>
      <FeatureTypeStyle>
        <Rule>
          <Name>rule1</Name>
          <PolygonSymbolizer>
            <Fill>
              <CssParameter name="fill">#3388ff</CssParameter>
            </Fill>
          </PolygonSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>`

	sld, err := ParseString(sldXML)
	if err != nil {
		t.Fatalf("Failed to parse SLD: %v", err)
	}

	style, err := sld.GetStyle("testlayer", "")
	if err != nil {
		t.Fatalf("Failed to get style: %v", err)
	}

	if style.Name != "default" {
		t.Errorf("Expected style name 'default', got '%s'", style.Name)
	}

	if len(style.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(style.Rules))
	}
}

func TestParseRejectsContentAfterRoot(t *testing.T) {
	const root = `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor version="1.0.0"><NamedLayer><Name>layer</Name><UserStyle><FeatureTypeStyle><Rule>
<PolygonSymbolizer><Fill><CssParameter name="fill">#5fa8cc</CssParameter></Fill></PolygonSymbolizer>
</Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	accepted := map[string]string{
		"nothing":     "",
		"whitespace":  "\n\n  \t",
		"comment":     "\n<!-- trailing note -->\n",
		"instruction": "\n<?editor keep?>",
	}
	for name, suffix := range accepted {
		if _, err := ParseString(root + suffix); err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
	}
	rejected := map[string]struct {
		suffix string
		line   int
	}{
		"unterminated element": {"\n<broken", 5},
		"second element":       {"<NamedLayer/>", 4},
		"text":                 {"\n  trailing text", 5},
	}
	for name, tc := range rejected {
		_, err := ParseString(root + tc.suffix)
		var parseErr *ParseError
		if !errors.As(err, &parseErr) {
			t.Errorf("%s: expected ParseError, got %v", name, err)
			continue
		}
		if parseErr.Line != tc.line {
			t.Errorf("%s: expected error on line %d, got line %d", name, tc.line, parseErr.Line)
		}
	}
	_, diagnostics, err := Compile(FormatSLD100, root+"\n<broken")
	if err == nil || len(diagnostics) != 1 || diagnostics[0].Code != "parse-error" || diagnostics[0].Line == 0 {
		t.Fatalf("expected positioned parse diagnostic, got %v %+v", err, diagnostics)
	}
}
