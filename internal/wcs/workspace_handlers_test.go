package wcs

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type fakeCoverageSource struct {
	info          datasource.CoverageInfo
	window        datasource.CoverageWindow
	renderRequest datasource.CoverageRenderRequest
}

func (f *fakeCoverageSource) Type() store.ServiceType      { return store.ServiceTypeRasterFile }
func (f *fakeCoverageSource) ID() string                   { return "raster" }
func (f *fakeCoverageSource) Health(context.Context) error { return nil }
func (f *fakeCoverageSource) Close() error                 { return nil }
func (f *fakeCoverageSource) DiscoverCoverages(context.Context) ([]*datasource.DiscoveredCoverage, error) {
	return []*datasource.DiscoveredCoverage{{SourceCoverage: "raster", Info: f.info}}, nil
}
func (f *fakeCoverageSource) GetCoverageInfo(context.Context, string) (*datasource.CoverageInfo, error) {
	return &f.info, nil
}
func (f *fakeCoverageSource) ExtractCoverage(_ context.Context, _ string, w datasource.CoverageWindow) ([]byte, error) {
	f.window = w
	return []byte("II*\x00fixture"), nil
}
func (f *fakeCoverageSource) ReadCoverage(_ context.Context, _ string, w datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	f.window = w
	values := make([]float64, w.Width*w.Height)
	for i := range values {
		values[i] = float64(i)
	}
	return &datasource.CoverageRaster{Width: w.Width, Height: w.Height, Bands: [][]float64{values}}, nil
}
func (f *fakeCoverageSource) RenderCoverage(_ context.Context, _ string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	f.renderRequest = request
	count := request.Width * request.Height
	bands := request.Bands
	if len(bands) == 0 {
		bands = make([]int, len(f.info.Bands))
		for i := range bands {
			bands[i] = i + 1
		}
	}
	result := &datasource.CoverageRenderGrid{Width: request.Width, Height: request.Height, BandNumbers: append([]int(nil), bands...), Bands: make([][]float64, len(bands)), BandInfo: make([]datasource.CoverageBand, len(bands)), Valid: make([]bool, count)}
	for i := range result.Valid {
		result.Valid[i] = true
	}
	for i, number := range bands {
		result.Bands[i] = make([]float64, count)
		for sample := range result.Bands[i] {
			result.Bands[i][sample] = float64(number*100 + sample)
		}
		result.BandInfo[i] = f.info.Bands[number-1]
	}
	return result, nil
}

func testHandler() (*handler, *workspace.Workspace, *fakeCoverageSource) {
	source := &fakeCoverageSource{info: datasource.CoverageInfo{CRS: "http://www.opengis.net/def/crs/EPSG/0/4326", SRID: 4326, AxisLabels: [2]string{"x", "y"}, Width: 4, Height: 3, OriginX: 10, OriginY: 20, ResolutionX: 1, ResolutionY: -1, Envelope: [4]float64{10, 17, 14, 20}, Bands: []datasource.CoverageBand{{Band: 1, Name: "value", DataType: "Float32"}}}}
	svc := &workspace.Service{ID: "svc", Enabled: true, CoverageSource: source, Coverages: map[string]*workspace.Coverage{"demo": {ID: "cov", SourceCoverage: "raster", PublicID: "demo", Title: "Demo", Enabled: true, Public: true}}}
	ws := &workspace.Workspace{ID: "ws", Name: "demo", Services: map[string]*workspace.Service{"svc": svc}, Settings: &store.WorkspaceSettings{WCS: store.WCSSettings{Enabled: true, Public: true}}}
	cfg := conf.Config{Server: conf.Server{UrlBase: "https://example.test"}, WCS: conf.WCS{Enabled: true, MaxCells: 100, MaxOutputBytes: 1024 * 1024, QueueTimeoutMS: 100, ProcessingTimeoutMS: 1000}}
	return &handler{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), slots: make(chan struct{}, 1)}, ws, source
}

func perform(h *handler, ws *workspace.Workspace, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	h.handle(rec, req)
	return rec
}

func performPost(h *handler, ws *workspace.Workspace, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/wcs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	req = req.WithContext(workspace.WithWorkspace(req.Context(), ws))
	rec := httptest.NewRecorder()
	h.handlePost(rec, req)
	return rec
}

func TestKVPVersionNegotiationAndCapabilities(t *testing.T) {
	h, ws, _ := testHandler()
	for _, version := range []string{"2.1.0", "2.0.1"} {
		rec := perform(h, ws, "/wcs?service=wcs&request=GetCapabilities&acceptversions="+version)
		if rec.Code != 200 {
			t.Fatalf("%s status=%d body=%s", version, rec.Code, rec.Body.String())
		}
		var root struct {
			XMLName xml.Name `xml:"Capabilities"`
			Version string   `xml:"version,attr"`
		}
		if err := xml.Unmarshal(rec.Body.Bytes(), &root); err != nil {
			t.Fatalf("invalid capabilities XML: %v", err)
		}
		if root.Version != version {
			t.Fatalf("got version %q", root.Version)
		}
		if !strings.Contains(rec.Body.String(), "demo") {
			t.Fatal("published coverage missing")
		}
		if version == "2.0.1" && !strings.Contains(rec.Body.String(), "WCS_protocol-binding_get-kvp/1.0/conf/get-kvp") {
			t.Fatal("WCS 2.0 KVP conformance identifier missing")
		}
		if !strings.Contains(rec.Body.String(), `<ows:BoundingBox crs="http://www.opengis.net/def/crs/EPSG/0/4326" dimensions="2">`) {
			t.Fatal("coverage bounding box missing")
		}
	}
}

func TestGetCoverageSubsetAndFormats(t *testing.T) {
	h, ws, source := testHandler()
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&FORMAT=image/tiff&SUBSET=x(11.5,12.5)&SUBSET=y(18.5)")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/tiff" {
		t.Fatalf("content type=%s", got)
	}
	want := datasource.CoverageWindow{XOff: 1, YOff: 1, Width: 2, Height: 1}
	if source.window != want {
		t.Fatalf("window=%+v want=%+v", source.window, want)
	}
	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=demo&FORMAT=application/gml%2Bxml")
	if rec.Code != 200 {
		t.Fatalf("gml status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &struct{ XMLName xml.Name }{}); err != nil {
		t.Fatalf("invalid GML: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "RectifiedGridCoverage") {
		t.Fatal("expected CIS 1.0 coverage")
	}
}

func TestInvalidSliceAndLimits(t *testing.T) {
	h, ws, _ := testHandler()
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&SUBSET=x(11.6)")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "InvalidSubsetting") {
		t.Fatalf("unexpected response %d %s", rec.Code, rec.Body.String())
	}
	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=demo&MEDIATYPE=invalid")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `locator="mediaType"`) {
		t.Fatalf("invalid media type response=%d %s", rec.Code, rec.Body.String())
	}
	h.cfg.WCS.MaxCells = 2
	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo")
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "OperationProcessingFailed") {
		t.Fatalf("unexpected limit response %d %s", rec.Code, rec.Body.String())
	}
}

func TestDimensionAliasesAndAxisValueLimits(t *testing.T) {
	_, ws, source := testHandler()
	ws.Settings.WCS.Extensions = []string{extMultidim}
	coverage := ws.Services["svc"].Coverages["demo"]
	coverage.Dimensions = []*workspace.Dimension{{Name: "time", SourceAxis: "forecast_time"}}
	query := url.Values{
		"SUBSET":     []string{`time("2026-07-01T00:00:00Z")`},
		"COVERAGEID": []string{"demo"},
	}
	plan, requestErr := buildCoveragePlan(query, "2.1.0", &source.info, coverage, newCapabilityRegistry("2.1.0", ws.Settings.WCS))
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if len(plan.query.DomainSubsets) != 1 || plan.query.DomainSubsets[0].Axis != "forecast_time" {
		t.Fatalf("domain subsets=%+v", plan.query.DomainSubsets)
	}

	instant := func(value string) datasource.CoverageAxisValue {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return datasource.CoverageAxisValue{Time: &parsed}
	}
	descriptor := &datasource.CoverageDescriptor{Axes: []datasource.CoverageAxis{
		{Label: "x", Kind: datasource.CoverageAxisSpatialX, GridHigh: 3},
		{Label: "y", Kind: datasource.CoverageAxisSpatialY, GridHigh: 2},
		{Label: "forecast_time", Kind: datasource.CoverageAxisTime, GridHigh: 2, Coordinates: []datasource.CoverageAxisValue{
			instant("2026-07-01T00:00:00Z"), instant("2026-07-02T00:00:00Z"), instant("2026-07-03T00:00:00Z"),
		}},
	}}
	if got := retainedAxisValues(descriptor, nil); got != 3 {
		t.Fatalf("retained values=%d want=3", got)
	}
	if got := retainedAxisValues(descriptor, plan.query.DomainSubsets); got != 1 {
		t.Fatalf("sliced retained values=%d want=1", got)
	}
}

func TestDescribeCoverage21(t *testing.T) {
	h, ws, _ := testHandler()
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=DescribeCoverage&COVERAGEID=demo")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &struct{ XMLName xml.Name }{}); err != nil {
		t.Fatalf("invalid description: %v", err)
	}
	for _, want := range []string{"CoverageDescription", "GeneralGridCoverage", "value"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestDescribeCoverage20SchemaStructure(t *testing.T) {
	h, ws, source := testHandler()
	source.info.Bands[0].NilValues = []string{"-9999", "NaN"}
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=DescribeCoverage&COVERAGEID=demo")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`<wcs:CoverageDescription gml:id="demo">`, "<gml:boundedBy>", "<wcs:CoverageId>demo</wcs:CoverageId>", "<gml:domainSet>", "<gmlcov:rangeType>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("description missing %q: %s", want, body)
		}
	}
	if !(strings.Index(body, "<gml:boundedBy>") < strings.Index(body, "<wcs:CoverageId>") && strings.Index(body, "<wcs:CoverageId>") < strings.Index(body, "<gml:domainSet>")) {
		t.Fatalf("coverage description elements are out of schema order: %s", body)
	}
	if strings.Count(body, "<swe:nilValues>") != 1 || strings.Index(body, "<swe:nilValues>") > strings.Index(body, "<swe:uom") {
		t.Fatalf("SWE nil values are not grouped before uom: %s", body)
	}
}

func TestDescribeCoverage20JoinsMultipleDescriptions(t *testing.T) {
	h, ws, _ := testHandler()
	ws.Services["svc"].Coverages["other"] = &workspace.Coverage{
		ID: "other", SourceCoverage: "raster", PublicID: "other", Title: "Other", Enabled: true, Public: true,
	}
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=DescribeCoverage&COVERAGEID=demo,other")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Count(body, "<wcs:CoverageDescriptions") != 1 || strings.Count(body, "<wcs:CoverageDescription ") != 2 {
		t.Fatalf("invalid joined coverage descriptions: %s", body)
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &struct{ XMLName xml.Name }{}); err != nil {
		t.Fatalf("joined description is not well-formed XML: %v\n%s", err, body)
	}
}

func TestWCS20CoverageSubtypeRepresentations(t *testing.T) {
	h, ws, _ := testHandler()
	ws.Services["svc"].Coverages["generic"] = &workspace.Coverage{
		ID: "generic", SourceCoverage: "raster", PublicID: "generic", Title: "Generic", Enabled: true, Public: true,
		WCS20CoverageSubtype: store.WCS20CoverageSubtypeGrid,
	}

	rec := perform(h, ws, "/wcs?SERVICE=WCS&REQUEST=GetCapabilities&ACCEPTVERSIONS=2.0.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities status=%d body=%s", rec.Code, rec.Body.String())
	}
	var caps struct {
		Summaries []struct {
			ID      string `xml:"CoverageId"`
			Subtype string `xml:"CoverageSubtype"`
		} `xml:"Contents>CoverageSummary"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	subtypes := map[string]string{}
	for _, summary := range caps.Summaries {
		subtypes[summary.ID] = summary.Subtype
	}
	if subtypes["demo"] != store.WCS20CoverageSubtypeRectifiedGrid || subtypes["generic"] != store.WCS20CoverageSubtypeGrid {
		t.Fatalf("coverage subtypes=%v", subtypes)
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=DescribeCoverage&COVERAGEID=generic")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `<gml:Grid dimension="2"`) || strings.Contains(body, "RectifiedGrid") || !strings.Contains(body, `<wcs:CoverageSubtype>GridCoverage</wcs:CoverageSubtype>`) {
		t.Fatalf("generic description=%d %s", rec.Code, body)
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=generic&FORMAT=application/gml%2Bxml")
	body = rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `<gmlcov:GridCoverage`) || !strings.Contains(body, `<gml:Grid dimension="2"`) || strings.Contains(body, "RectifiedGrid") {
		t.Fatalf("generic GML coverage=%d %s", rec.Code, body)
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &struct{ XMLName xml.Name }{}); err != nil {
		t.Fatalf("generic GML is not well formed: %v", err)
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=generic&MEDIATYPE=multipart%2Frelated")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "multipart/related") || !strings.Contains(rec.Body.String(), `<gmlcov:GridCoverage`) {
		t.Fatalf("generic multipart=%d %s", rec.Code, rec.Body.String())
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&REQUEST=GetCapabilities&ACCEPTVERSIONS=2.1.0")
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `<wcs:CoverageSubtype>GeneralGridCoverage</wcs:CoverageSubtype>`) != 2 {
		t.Fatalf("WCS 2.1 capabilities changed: %d %s", rec.Code, rec.Body.String())
	}
	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=generic&FORMAT=application/gml%2Bxml")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<cis:GeneralGridCoverage`) {
		t.Fatalf("WCS 2.1 coverage changed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestWCS20ScalingRunsForBothCoverageSubtypes(t *testing.T) {
	h, ws, source := testHandler()
	ws.Settings.WCS.Extensions = []string{extScaling}
	ws.Services["svc"].Coverages["generic"] = &workspace.Coverage{
		ID: "generic", SourceCoverage: "raster", PublicID: "generic", Title: "Generic", Enabled: true, Public: true,
		WCS20CoverageSubtype: store.WCS20CoverageSubtypeGrid,
	}
	tests := []struct {
		parameter string
		width     int
		height    int
	}{
		{parameter: "SCALEFACTOR=0.5", width: 2, height: 2},
		{parameter: "SCALEAXES=i(0.5),j(2)", width: 2, height: 6},
		{parameter: "SCALESIZE=i(2),j(4)", width: 2, height: 4},
		{parameter: "SCALEEXTENT=i(2:4),j(5:6)", width: 3, height: 2},
	}
	for _, coverageID := range []string{"demo", "generic"} {
		for _, test := range tests {
			name := coverageID + "/" + strings.SplitN(test.parameter, "=", 2)[0]
			t.Run(name, func(t *testing.T) {
				parameter := strings.SplitN(test.parameter, "=", 2)
				target := "/wcs?SERVICE=WCS&VERSION=2.0.1&REQUEST=GetCoverage&COVERAGEID=" + coverageID + "&FORMAT=application/gml%2Bxml&" + parameter[0] + "=" + url.QueryEscape(parameter[1])
				rec := perform(h, ws, target)
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				if source.renderRequest.Width != test.width || source.renderRequest.Height != test.height {
					t.Fatalf("render size=%dx%d want=%dx%d", source.renderRequest.Width, source.renderRequest.Height, test.width, test.height)
				}
				root := "RectifiedGridCoverage"
				if coverageID == "generic" {
					root = "GridCoverage"
				}
				if !strings.Contains(rec.Body.String(), `<gmlcov:`+root) {
					t.Fatalf("coverage root does not match subtype: %s", rec.Body.String())
				}
			})
		}
	}
}

func TestWCS21AnnexA(t *testing.T) {
	h, ws, _ := testHandler()
	t.Run("getCapabilities-cis11", func(t *testing.T) {
		rec := perform(h, ws, "/wcs?SERVICE=WCS&REQUEST=GetCapabilities&ACCEPTVERSIONS=2.1.0")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "GeneralGridCoverage") {
			t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("describeCoverage-cis11", func(t *testing.T) {
		rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=DescribeCoverage&COVERAGEID=demo")
		for _, value := range []string{"cis:envelope", "cis:domainSet", "cis:rangeType"} {
			if !strings.Contains(rec.Body.String(), value) {
				t.Fatalf("missing %s", value)
			}
		}
	})
	t.Run("describeCoverage-cis11-no-nesting", func(t *testing.T) {
		rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=DescribeCoverage&COVERAGEID=demo")
		if strings.Contains(rec.Body.String(), "PartitionSet") || !strings.Contains(rec.Body.String(), "cis:envelope") {
			t.Fatal("invalid nested description")
		}
	})
	t.Run("getCoverage-cis11", func(t *testing.T) {
		rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&FORMAT=application/gml%2Bxml")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "cis:GeneralGridCoverage") {
			t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestOptionalExtensionsAreAdvertisedAndExecuted(t *testing.T) {
	h, ws, source := testHandler()
	ws.Settings.WCS.Extensions = []string{extXMLPost, extRangeSubset, extScaling, extCRS, extInterpolation}
	ws.Settings.WCS.AllowedOutputCRS = []string{"http://www.opengis.net/def/crs/EPSG/0/3857"}
	ws.Settings.WCS.InterpolationMethods = []string{"nearest-neighbor", "linear"}
	source.info.Bands = []datasource.CoverageBand{
		{Band: 1, Name: "red", DataType: "Float32"},
		{Band: 2, Name: "green", DataType: "Float32"},
		{Band: 3, Name: "blue", DataType: "Float32"},
	}

	rec := perform(h, ws, "/wcs?SERVICE=WCS&REQUEST=GetCapabilities&ACCEPTVERSIONS=2.0.1")
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"WCS_protocol-binding_post-xml", "range-subsetting", "service-extension_scaling", "service-extension_crs", "service-extension_interpolation", "ows:Post", "InterpolationMetadata", "CrsMetadata"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("capabilities missing %q", want)
		}
	}
	if count := strings.Count(rec.Body.String(), "<wcs:Extension>"); count != 1 {
		t.Fatalf("ServiceMetadata must contain one extension container, got %d: %s", count, rec.Body.String())
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&FORMAT=application/gml%2Bxml&RANGESUBSET=blue,red&SCALESIZE=i(2),j(4)&INTERPOLATION=nearest-neighbor")
	if rec.Code != http.StatusOK {
		t.Fatalf("GetCoverage status=%d body=%s", rec.Code, rec.Body.String())
	}
	if source.renderRequest.Width != 2 || source.renderRequest.Height != 4 || source.renderRequest.Resampling != "nearest" {
		t.Fatalf("render request=%+v", source.renderRequest)
	}
	if len(source.renderRequest.Bands) != 2 || source.renderRequest.Bands[0] != 3 || source.renderRequest.Bands[1] != 1 {
		t.Fatalf("band order=%v", source.renderRequest.Bands)
	}
	blue := strings.Index(rec.Body.String(), `name="blue"`)
	red := strings.Index(rec.Body.String(), `name="red"`)
	if blue < 0 || red < blue {
		t.Fatalf("range order not preserved: %s", rec.Body.String())
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&FORMAT=application/gml%2Bxml&OUTPUTCRS=http%3A%2F%2Fwww.opengis.net%2Fdef%2Fcrs%2FEPSG%2F0%2F3857")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "3857") {
		t.Fatalf("CRS response=%d %s", rec.Code, rec.Body.String())
	}

	rec = perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&SCALEAXES=i(NaN)")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "InvalidParameterValue") {
		t.Fatalf("non-finite scale response=%d %s", rec.Code, rec.Body.String())
	}
}

func TestXMLPOSTUsesCanonicalPipeline(t *testing.T) {
	h, ws, source := testHandler()
	ws.Settings.WCS.Extensions = []string{extXMLPost, extScaling, extInterpolation}
	body := `<wcs:GetCoverage xmlns:wcs="http://www.opengis.net/wcs/2.0" xmlns:scal="http://www.opengis.net/wcs/scaling/1.0" xmlns:int="http://www.opengis.net/wcs/interpolation/1.0" service="WCS" version="2.0.1">
		<wcs:CoverageId>demo</wcs:CoverageId>
		<wcs:DimensionTrim><wcs:Dimension>x</wcs:Dimension><wcs:TrimLow>10.5</wcs:TrimLow><wcs:TrimHigh>12.5</wcs:TrimHigh></wcs:DimensionTrim>
		<wcs:Extension><scal:ScaleToSize><scal:TargetAxisSize><scal:axis>i</scal:axis><scal:targetSize>2</scal:targetSize></scal:TargetAxisSize><scal:TargetAxisSize><scal:axis>j</scal:axis><scal:targetSize>2</scal:targetSize></scal:TargetAxisSize></scal:ScaleToSize><int:Interpolation><int:globalInterpolation>http://www.opengis.net/def/interpolation/OGC/1/linear</int:globalInterpolation></int:Interpolation></wcs:Extension>
		<wcs:format>application/gml+xml</wcs:format>
	</wcs:GetCoverage>`
	rec := performPost(h, ws, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if source.renderRequest.Width != 2 || source.renderRequest.Height != 2 || source.renderRequest.Resampling != "bilinear" {
		t.Fatalf("render request=%+v", source.renderRequest)
	}
}

func TestExtensionsRemainOptIn(t *testing.T) {
	h, ws, _ := testHandler()
	rec := perform(h, ws, "/wcs?SERVICE=WCS&VERSION=2.1.0&REQUEST=GetCoverage&COVERAGEID=demo&RANGESUBSET=value")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "OperationNotSupported") {
		t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
	}
	rec = performPost(h, ws, `<wcs:GetCapabilities xmlns:wcs="http://www.opengis.net/wcs/2.0" service="WCS"/>`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "OperationNotSupported") {
		t.Fatalf("POST response=%d %s", rec.Code, rec.Body.String())
	}
}

func TestXMLPOSTRejectsDirectives(t *testing.T) {
	h, ws, _ := testHandler()
	ws.Settings.WCS.Extensions = []string{extXMLPost}
	rec := performPost(h, ws, `<!DOCTYPE wcs [<!ENTITY x SYSTEM "file:///etc/passwd">]><GetCapabilities xmlns="http://www.opengis.net/wcs/2.0" service="WCS">&x;</GetCapabilities>`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "directives") {
		t.Fatalf("response=%d %s", rec.Code, rec.Body.String())
	}
}
