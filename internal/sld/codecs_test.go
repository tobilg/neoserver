package sld

import "testing"

func TestAlternativeFormatsCompileToCanonicalDocument(t *testing.T) {
	tests := []struct {
		name, format, body string
	}{
		{"css", FormatCSS, `* { fill: #336699; fill-opacity: 0.5; stroke: #ffffff; stroke-width: 2; label: [name]; text-fill: #111111; }`},
		{"ysld", FormatYSLD, `name: roads
feature-styles:
  - rules:
      - name: road
        symbolizers:
          - line:
              color: "#334455"
              width: 3
          - text:
              label: "[name]"
              color: "#000000"
`},
		{"mapbox", FormatMapbox, `{"version":8,"name":"places","sources":{},"layers":[{"id":"places","type":"circle","paint":{"circle-radius":6,"circle-color":"#ff0000"}},{"id":"names","type":"symbol","layout":{"text-field":["get","name"],"text-size":14},"paint":{"text-color":"#111111"}}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, diagnostics, err := Compile(test.format, test.body)
			if err != nil {
				t.Fatalf("Compile() error = %v; diagnostics = %#v", err, diagnostics)
			}
			style, err := doc.GetDefaultStyle()
			if err != nil {
				t.Fatal(err)
			}
			if len(style.Rules) == 0 {
				t.Fatal("compiled style has no rules")
			}
		})
	}
}

func TestAlternativeFormatsRejectUnsupportedProperties(t *testing.T) {
	_, diagnostics, err := Compile(FormatCSS, `* { made-up-property: true; }`)
	if err == nil || len(diagnostics) != 1 || diagnostics[0].Code != "compile-error" {
		t.Fatalf("expected compile diagnostic, got err=%v diagnostics=%#v", err, diagnostics)
	}
}
