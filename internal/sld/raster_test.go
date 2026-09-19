package sld

import "testing"

func TestRasterSymbolizerAndPainterOrder(t *testing.T) {
	xml := `<StyledLayerDescriptor version="1.0.0"><NamedLayer><Name>dem</Name><UserStyle><Name>terrain</Name><FeatureTypeStyle><Rule><LineSymbolizer><Stroke><CssParameter name="stroke">#000000</CssParameter></Stroke></LineSymbolizer><RasterSymbolizer><Opacity>0.75</Opacity><ChannelSelection><GrayChannel><SourceChannelName>elevation</SourceChannelName></GrayChannel></ChannelSelection><ColorMap type="intervals"><ColorMapEntry color="#0000ff" quantity="0"/><ColorMapEntry color="#ff0000" quantity="100" opacity="0.5"/></ColorMap></RasterSymbolizer><TextSymbolizer><Label><PropertyName>name</PropertyName></Label></TextSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`
	doc, err := ParseString(xml)
	if err != nil {
		t.Fatal(err)
	}
	if err = Validate(doc); err != nil {
		t.Fatal(err)
	}
	style, err := doc.GetStyle("dem", "terrain")
	if err != nil {
		t.Fatal(err)
	}
	symbols := style.Rules[0].Symbolizers
	if len(symbols) != 3 || symbols[0].Line == nil || symbols[1].Raster == nil || symbols[2].Text == nil {
		t.Fatalf("symbolizer order lost: %+v", symbols)
	}
	raster := symbols[1].Raster
	if raster.Opacity != 0.75 || raster.ColorMap.Type != "intervals" || raster.Channels.Gray.Name != "elevation" {
		t.Fatalf("unexpected raster style: %+v", raster)
	}
}

func TestUnsupportedRasterConstructRejected(t *testing.T) {
	doc, err := ParseString(`<StyledLayerDescriptor><NamedLayer><Name>x</Name><UserStyle><FeatureTypeStyle><Rule><RasterSymbolizer><ShadedRelief/></RasterSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`)
	if err != nil {
		t.Fatal(err)
	}
	if Validate(doc) == nil {
		t.Fatal("expected ShadedRelief rejection")
	}
}

func TestHistogramAndNormalizeVendorOptionsResolve(t *testing.T) {
	doc, err := ParseString(`<StyledLayerDescriptor><NamedLayer><Name>x</Name><UserStyle><FeatureTypeStyle>
<Rule><RasterSymbolizer><ContrastEnhancement><Histogram/><GammaValue>2</GammaValue></ContrastEnhancement></RasterSymbolizer></Rule>
<Rule><RasterSymbolizer><ContrastEnhancement><Normalize><VendorOption name="algorithm">StretchToMinimumMaximum</VendorOption><VendorOption name="minValue">10</VendorOption><VendorOption name="maxValue">20</VendorOption></Normalize></ContrastEnhancement></RasterSymbolizer></Rule>
</FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`)
	if err != nil {
		t.Fatal(err)
	}
	style, err := doc.GetDefaultStyle()
	if err != nil {
		t.Fatal(err)
	}
	if !style.Rules[0].RasterStyle.ContrastEnhancement.Histogram || style.Rules[0].RasterStyle.ContrastEnhancement.Gamma != 2 {
		t.Fatalf("histogram was not resolved: %+v", style.Rules[0].RasterStyle.ContrastEnhancement)
	}
	normalize := style.Rules[1].RasterStyle.ContrastEnhancement
	if normalize.Algorithm != "StretchToMinimumMaximum" || normalize.MinValue == nil || *normalize.MinValue != 10 || normalize.MaxValue == nil || *normalize.MaxValue != 20 {
		t.Fatalf("normalize options were not resolved: %+v", normalize)
	}
}

func TestInvalidNormalizeVendorOptionsRejected(t *testing.T) {
	for _, body := range []string{
		`<VendorOption name="algorithm">unknown</VendorOption><VendorOption name="minValue">1</VendorOption><VendorOption name="maxValue">2</VendorOption>`,
		`<VendorOption name="algorithm">ClipToZero</VendorOption><VendorOption name="minValue">2</VendorOption><VendorOption name="maxValue">1</VendorOption>`,
		`<VendorOption name="unexpected">1</VendorOption>`,
	} {
		doc, err := ParseString(`<StyledLayerDescriptor><NamedLayer><Name>x</Name><UserStyle><FeatureTypeStyle><Rule><RasterSymbolizer><ContrastEnhancement><Normalize>` + body + `</Normalize></ContrastEnhancement></RasterSymbolizer></Rule></FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`)
		if err != nil {
			t.Fatal(err)
		}
		if Validate(doc) == nil {
			t.Fatalf("expected invalid normalize options to fail: %s", body)
		}
	}
}

func TestDynamicRasterEnvironmentAndAdvancedFeatureOptions(t *testing.T) {
	doc, err := ParseString(`<StyledLayerDescriptor><NamedLayer><Name>x</Name><UserStyle><FeatureTypeStyle>
<Transformation><Function name="vec:Heatmap"><Function name="parameter"><Literal>radiusPixels</Literal><Literal>12</Literal></Function></Function></Transformation>
<VendorOption name="composite">multiply</VendorOption><VendorOption name="sortBy">rank D</VendorOption>
<Rule><RasterSymbolizer><Opacity><Function name="env"><Literal>opacity</Literal><Literal>0.5</Literal></Function></Opacity><ColorMap><ColorMapEntry color="#000000" quantity="0"/><ColorMapEntry color="#ffffff" quantity="${env('max', 10)}"/></ColorMap></RasterSymbolizer>
<TextSymbolizer><Label><PropertyName>name</PropertyName></Label><Priority>4</Priority><VendorOption name="spaceAround">8</VendorOption></TextSymbolizer></Rule>
</FeatureTypeStyle></UserStyle></NamedLayer></StyledLayerDescriptor>`)
	if err != nil {
		t.Fatal(err)
	}
	style, err := doc.GetDefaultStyle()
	if err != nil {
		t.Fatal(err)
	}
	if style.Transformation == nil || style.Transformation.Name != "heatmap" || style.Transformation.Radius != 12 || style.Composite != "multiply" || style.SortBy != "rank" || !style.SortDescending {
		t.Fatalf("options not resolved: %+v", style)
	}
	if !StyleUsesAdvancedLabels(style) {
		t.Fatal("advanced labels not detected")
	}
	raster := style.Rules[0].RasterStyle
	if !RasterStyleUsesEnvironment(raster) {
		t.Fatal("dynamic raster not detected")
	}
	resolved, err := ResolveRasterEnvironment(raster, map[string]string{"opacity": "0.75", "max": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Opacity != .75 || resolved.ColorMap.Entries[1].Quantity != 20 {
		t.Fatalf("environment not applied: %+v", resolved)
	}
}

func TestExpressionEvaluation(t *testing.T) {
	value, err := EvaluateNumericExpression(`${env('base', 2) * 3 + 1}`, map[string]string{"base": "4"})
	if err != nil || value != 13 {
		t.Fatalf("value=%v err=%v", value, err)
	}
	if _, err = EvaluateNumericExpression(`${1 / 0}`, nil); err == nil {
		t.Fatal("expected division by zero")
	}
	if _, err = EvaluateNumericExpression(`${env('value', 1)}`, map[string]string{"value": "NaN"}); err == nil {
		t.Fatal("expected non-finite environment value rejection")
	}
}
