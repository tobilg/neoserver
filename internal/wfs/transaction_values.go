package wfs

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

const transactionNamespaces = `<root xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`

var decimalText = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func transactionLayerInfo(ctx context.Context, service *workspace.Service, writer datasource.FeatureWriter, layer string) (*datasource.LayerInfo, error) {
	if returning, ok := writer.(datasource.ReturningFeatureWriter); ok {
		return returning.GetLayerInfo(ctx, layer)
	}
	return service.DataSource.GetLayerInfo(ctx, layer)
}

func (h *workspaceHandler) checkMutationLocks(ws *workspace.Workspace, layer, lockID string, ids []string) error {
	if h.state == nil || h.state.Locks == nil {
		return nil
	}
	publication, service := resolveFeatureLayer(ws, layer, h.cfg.WFS.AppNamespacePrefix)
	if publication == nil || service == nil {
		return fmt.Errorf("cannot resolve mutation lock identity")
	}
	return h.state.Locks.CheckFeatureLocks(ws.ID, sourceLockKey(service, publication, layer), ids, lockID)
}

// Scalar text is XML-decoded without trimming. Complex values are accepted
// only by the geometry parser, never silently stored as markup.
func scalarXML(value *WFSValue) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	if value.Nil != "" && value.Nil != "false" && value.Nil != "0" && value.Nil != "true" && value.Nil != "1" {
		return nil, fmt.Errorf("invalid xsi:nil value")
	}
	d := xml.NewDecoder(strings.NewReader(transactionNamespaces + value.RawXML + "</root>"))
	var text strings.Builder
	rootSeen := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch v := token.(type) {
		case xml.StartElement:
			if rootSeen {
				return nil, fmt.Errorf("complex XML is not a scalar property value")
			}
			rootSeen = true
		case xml.CharData:
			text.Write(v)
		case xml.Directive:
			return nil, fmt.Errorf("XML directives are not supported")
		}
	}
	if value.Nil == "true" || value.Nil == "1" {
		if strings.TrimSpace(text.String()) != "" {
			return nil, fmt.Errorf("a nil value cannot contain text")
		}
		return nil, nil
	}
	return text.String(), nil
}

func typedScalar(info *datasource.LayerInfo, name string, value interface{}) (interface{}, error) {
	text, ok := value.(string)
	if !ok {
		return value, nil
	}
	for _, p := range info.Properties {
		if p.Name != name {
			continue
		}
		switch p.JSONType {
		case datasource.JSONTypeInteger:
			return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		case datasource.JSONTypeNumber:
			nativeType := strings.ToLower(p.Type)
			if strings.HasPrefix(nativeType, "numeric") || strings.HasPrefix(nativeType, "decimal") {
				decimal := strings.TrimSpace(text)
				if !decimalText.MatchString(decimal) {
					return nil, fmt.Errorf("invalid decimal value")
				}
				// Keep the validated decimal text: converting through float64
				// would silently round exact PostgreSQL numeric columns.
				return decimal, nil
			}
			return strconv.ParseFloat(strings.TrimSpace(text), 64)
		case datasource.JSONTypeBoolean:
			return strconv.ParseBool(strings.TrimSpace(text))
		}
	}
	return value, nil
}

func nativeTransactionProperties(info *datasource.LayerInfo, values map[string]interface{}) (map[string]interface{}, error) {
	names, err := propertyXMLNames(info)
	if err != nil {
		return nil, err
	}
	result := make(map[string]interface{}, len(values))
	for name, value := range values {
		for native, advertised := range names {
			if name == advertised {
				name = native
				break
			}
		}
		if _, exists := names[name]; !exists {
			return nil, &RequestError{Code: ExceptionInvalidValue, Locator: name, Message: "unknown feature property"}
		}
		converted, err := typedScalar(info, name, value)
		if err != nil {
			return nil, &RequestError{Code: ExceptionInvalidValue, Locator: name, Message: err.Error()}
		}
		result[name] = converted
	}
	return result, nil
}

// Encode a resolved XML subtree with Go's encoder: namespace bindings and
// escaped characters survive extraction from an enclosing transaction.
func resolvedInnerXML(d *xml.Decoder, start xml.StartElement) (string, error) {
	var out strings.Builder
	e := xml.NewEncoder(&out)
	depth := 0
	for {
		token, err := d.Token()
		if err != nil {
			return "", err
		}
		if _, ok := token.(xml.EndElement); ok {
			if depth == 0 {
				break
			}
			depth--
		}
		if v, ok := token.(xml.StartElement); ok {
			depth++
			token = cleanXMLStart(v)
		}
		if _, ok := token.(xml.Directive); ok {
			return "", fmt.Errorf("XML directives are not supported")
		}
		if err := e.EncodeToken(token); err != nil {
			return "", err
		}
	}
	if err := e.Flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func cleanXMLStart(start xml.StartElement) xml.StartElement {
	attrs := make([]xml.Attr, 0, len(start.Attr))
	for _, a := range start.Attr {
		if a.Name.Space != "xmlns" && a.Name.Local != "xmlns" {
			attrs = append(attrs, a)
		}
	}
	start.Attr = attrs
	return start
}

func (v *WFSValue) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		if a.Name.Local == "nil" && a.Name.Space == "http://www.w3.org/2001/XMLSchema-instance" {
			v.Nil = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	v.RawXML = inner
	return err
}

func (f *XMLFeature) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	f.XMLName = start.Name
	for _, a := range start.Attr {
		if a.Name.Local == "id" {
			f.GmlId = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	f.Properties = inner
	return err
}

func (v *WFSReplace) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "handle":
			v.Handle = a.Value
		case "srsName":
			v.SrsName = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	v.InnerXML = inner
	return err
}

func (v *WFSUpdate) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "handle":
			v.Handle = a.Value
		case "typeName":
			v.TypeName = a.Value
		case "srsName":
			v.SrsName = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	if err != nil {
		return err
	}
	v.FilterRaw = inner
	if err := validateMutationChildren(inner, true); err != nil {
		return err
	}
	var properties struct {
		Properties []WFSProperty `xml:"Property"`
	}
	if err := xml.Unmarshal([]byte("<root>"+inner+"</root>"), &properties); err != nil {
		return err
	}
	v.Properties = properties.Properties
	return nil
}

func (v *WFSDelete) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "handle":
			v.Handle = a.Value
		case "typeName":
			v.TypeName = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	v.FilterRaw = inner
	if err == nil {
		err = validateMutationChildren(inner, false)
	}
	return err
}

func validateMutationChildren(inner string, properties bool) error {
	d := xml.NewDecoder(strings.NewReader("<root>" + inner + "</root>"))
	if _, err := d.Token(); err != nil {
		return err
	}
	for {
		token, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !((start.Name.Local == "Property" && properties && (start.Name.Space == "" || start.Name.Space == NSWfs)) || (start.Name.Local == "Filter" && (start.Name.Space == "" || start.Name.Space == NSFes))) {
			return fmt.Errorf("unsupported mutation child {%s}%s", start.Name.Space, start.Name.Local)
		}
		if err := d.Skip(); err != nil {
			return err
		}
	}
}

func (v *XMLQuery) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "typeNames":
			v.TypeNames = a.Value
		case "srsName":
			v.SrsName = a.Value
		}
	}
	inner, err := resolvedInnerXML(d, start)
	v.FilterRaw = inner
	if err != nil {
		return err
	}
	var projection struct {
		Names []string `xml:"PropertyName"`
	}
	if err := xml.Unmarshal([]byte("<root>"+inner+"</root>"), &projection); err != nil {
		return err
	}
	v.PropertyNames = projection.Names
	return nil
}

func parseTransactionProperties(raw string) (map[string]interface{}, string, error) {
	props := map[string]interface{}{}
	d := xml.NewDecoder(strings.NewReader(transactionNamespaces + raw + "</root>"))
	if _, err := d.Token(); err != nil {
		return nil, "", err
	}
	var geometry string
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		var value WFSValue
		if err := d.DecodeElement(&value, &start); err != nil {
			return nil, "", err
		}
		scalar, scalarErr := scalarXML(&value)
		if scalarErr == nil {
			props[start.Name.Local] = scalar
			continue
		}
		if geometry != "" {
			return nil, "", fmt.Errorf("multiple geometry properties are not supported")
		}
		// Validation of the geometry subtree and CRS happens with layer context.
		if value.Nil == "true" || value.Nil == "1" {
			return nil, "", fmt.Errorf("nil geometry cannot contain XML")
		}
		geometry = value.RawXML
	}
	return props, geometry, nil
}

func normalizedInputCRS(value string) (string, int, error) {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "crs:84", "ogc:crs84", "urn:ogc:def:crs:ogc::crs84", "urn:ogc:def:crs:ogc:1.3:crs84", "http://www.opengis.net/def/crs/ogc/1.3/crs84", "https://www.opengis.net/def/crs/ogc/1.3/crs84":
		return "EPSG:4326", 4326, nil // Explicit longitude/latitude.
	}
	srid, err := crs.Parse(value)
	if err != nil || srid <= 0 {
		return "", 0, fmt.Errorf("unsupported input CRS %q", value)
	}
	if strings.HasPrefix(lower, "epsg:") {
		return crs.ToEPSG(srid), srid, nil
	}
	// Native GML ingestion honors formal EPSG axis order for URNs. Normalize
	// equivalent HTTP forms to that supported representation.
	return crs.ToURN(srid), srid, nil
}

func (h *workspaceHandler) transactionGeometry(raw, inherited string) (datasource.GeometryValue, error) {
	defaultCRS := h.cfg.WFS.DefaultSRS
	if defaultCRS == "" {
		defaultCRS = crs.ToURN(4326)
	}
	_, defaultSRID, err := normalizedInputCRS(defaultCRS)
	if err != nil {
		return datasource.GeometryValue{}, err
	}
	if inherited == "" {
		inherited = defaultCRS
	}
	var out strings.Builder
	e := xml.NewEncoder(&out)
	d := xml.NewDecoder(strings.NewReader(transactionNamespaces + raw + "</root>"))
	if _, err := d.Token(); err != nil {
		return datasource.GeometryValue{}, err
	}
	depth, roots, sourceSRID := 0, 0, 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return datasource.GeometryValue{}, err
		}
		switch v := token.(type) {
		case xml.StartElement:
			v = cleanXMLStart(v)
			if depth == 0 {
				roots++
				if roots != 1 || !isGeometryElement(v.Name.Local) {
					return datasource.GeometryValue{}, fmt.Errorf("expected one supported GML geometry")
				}
				explicit := false
				for _, a := range v.Attr {
					explicit = explicit || a.Name.Local == "srsName"
				}
				if !explicit {
					v.Attr = append(v.Attr, xml.Attr{Name: xml.Name{Local: "srsName"}, Value: inherited})
				}
			}
			for i, a := range v.Attr {
				if a.Name.Local == "href" && !strings.HasPrefix(a.Value, "#") {
					return datasource.GeometryValue{}, fmt.Errorf("external geometry references are not supported")
				}
				if a.Name.Local != "srsName" {
					continue
				}
				normalized, srid, err := normalizedInputCRS(a.Value)
				if err != nil || (srid != defaultSRID && srid != 3857) {
					return datasource.GeometryValue{}, fmt.Errorf("input CRS %q is not advertised for this feature type", a.Value)
				}
				v.Attr[i].Value = normalized
				if depth == 0 {
					sourceSRID = srid
				}
			}
			depth++
			token = v
		case xml.EndElement:
			if depth == 0 {
				continue
			}
			depth--
		case xml.Directive:
			return datasource.GeometryValue{}, fmt.Errorf("XML directives are not supported")
		case xml.CharData:
			if depth == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return datasource.GeometryValue{}, fmt.Errorf("unexpected geometry text")
				}
				continue
			}
		}
		if err := e.EncodeToken(token); err != nil {
			return datasource.GeometryValue{}, err
		}
	}
	if roots != 1 {
		return datasource.GeometryValue{}, fmt.Errorf("geometry is empty")
	}
	if err := e.Flush(); err != nil {
		return datasource.GeometryValue{}, err
	}
	return datasource.GeometryValue{GML: out.String(), SRID: sourceSRID}, nil
}
