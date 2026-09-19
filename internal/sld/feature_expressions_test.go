package sld

import "testing"

func TestSymbolizerFunctionsResolvePerFeature(t *testing.T) {
	body := `<StyledLayerDescriptor version="1.0.0"><NamedLayer><Name>places</Name><UserStyle><FeatureTypeStyle><Rule><PointSymbolizer><Graphic><Mark><WellKnownName>circle</WellKnownName><Fill><CssParameter name="fill"><Function name="env"><Literal>color</Literal><Literal>#000000</Literal></Function></CssParameter></Fill></Mark><Size><Function name="property"><Literal>size</Literal></Function></Size></Graphic></PointSymbolizer><TextSymbolizer><Label><Function name="strConcat"><Literal>ID-</Literal><PropertyName>id</PropertyName></Function></Label></TextSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	doc, _, err := Compile(FormatSLD100, body)
	if err != nil {
		t.Fatal(err)
	}
	style, err := doc.GetDefaultStyle()
	if err != nil {
		t.Fatal(err)
	}
	rule := style.Rules[0]
	point, err := ResolveSymbolizerExpressions(rule.Symbolizers[0], map[string]interface{}{"size": 14}, map[string]string{"color": "#ff0000"})
	if err != nil {
		t.Fatal(err)
	}
	if point.Point.Size != 14 || point.Point.FillColor.R != 255 {
		t.Fatalf("point=%+v", point.Point)
	}
	text, err := ResolveSymbolizerExpressions(rule.Symbolizers[1], map[string]interface{}{"id": 7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text.Text.Literal != "ID-7" {
		t.Fatalf("label=%q", text.Text.Literal)
	}
}

func TestProcessRegistryAndRasterAlgebraValidation(t *testing.T) {
	definitions := RegisteredProcesses()
	if len(definitions) < 8 {
		t.Fatalf("processes=%v", definitions)
	}
	if definition, ok := LookupProcess("vec:PointStacker"); !ok || definition.Input != ProcessVector {
		t.Fatalf("definition=%+v ok=%v", definition, ok)
	}
}
