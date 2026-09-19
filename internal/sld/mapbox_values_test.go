package sld

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestMapboxRejectsUnsupportedAndInvalidPaint(t *testing.T) {
	for _, value := range []string{`["interpolate",["linear"],["zoom"],0,2,12,20]`, `["step",["get","size"],2,12,20]`, `["+",1,2]`, `[1,2]`, `[]`, `["get",""]`, `-1`, `true`, `"large"`, `"NaN"`, `null`} {
		t.Run(value, func(t *testing.T) {
			_, diagnostics, err := Compile(FormatMapbox, fmt.Sprintf(`{"version":8,"layers":[{"id":"places","type":"circle","paint":{"circle-radius":%s}}]}`, value))
			if err == nil || len(diagnostics) != 1 || diagnostics[0].Path != "layers[0].paint.circle-radius" {
				t.Fatalf("err=%v diagnostics=%+v", err, diagnostics)
			}
		})
	}
	for property, value := range map[string]any{"circle-opacity": 1.1, "circle-color": "#zzzzzz", "circle-stroke-width": -2} {
		body, _ := json.Marshal(map[string]any{"layers": []any{map[string]any{"type": "circle", "paint": map[string]any{property: value}}}})
		if _, _, err := Compile(FormatMapbox, string(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestMapboxRadiusAndPropertyExpressions(t *testing.T) {
	for _, radius := range []string{`6`, `["get","radius"]`} {
		doc, _, err := Compile(FormatMapbox, fmt.Sprintf(`{"layers":[{"type":"circle","paint":{"circle-radius":%s,"circle-color":["get","color"]}},{"type":"symbol","layout":{"text-field":["get","name"]}}]}`, radius))
		if err != nil {
			t.Fatal(err)
		}
		style, err := doc.GetDefaultStyle()
		if err != nil {
			t.Fatal(err)
		}
		point, err := ResolveSymbolizerExpressions(style.Rules[0].Symbolizers[0], map[string]interface{}{"radius": 6, "color": "#ff0000"}, nil)
		if err != nil || point.Point.Size != 12 || point.Point.FillColor.R != 255 {
			t.Fatalf("point=%+v err=%v", point.Point, err)
		}
		text, err := ResolveSymbolizerExpressions(style.Rules[1].Symbolizers[0], map[string]interface{}{"name": "Berlin"}, nil)
		if err != nil || text.Text.Literal != "Berlin" {
			t.Fatalf("text=%+v err=%v", text.Text, err)
		}
	}
}

func TestMapboxLiteralDashArrays(t *testing.T) {
	for _, value := range []string{`[2,4]`, `["get","dash"]`, `["step",["zoom"],1,2,3]`, `[0,0]`, `[-1,2]`} {
		doc, _, err := Compile(FormatMapbox, fmt.Sprintf(`{"layers":[{"type":"line","paint":{"line-dasharray":%s}}]}`, value))
		if value != `[2,4]` {
			if err == nil {
				t.Fatalf("accepted %s", value)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		style, err := doc.GetDefaultStyle()
		if err != nil {
			t.Fatal(err)
		}
		if len(style.Rules[0].Symbolizers[0].Line.DashArray) != 2 {
			t.Fatal("lost literal dash array")
		}
	}
}
