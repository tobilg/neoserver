package wcs

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/tobilg/neoserver/internal/workspace"
)

const maxWCSXMLBody = int64(2 << 20)

type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []xmlNode  `xml:",any"`
}

func (h *handler) handlePost(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok || ws.Settings == nil || !ws.Settings.WCS.Enabled {
		writeException(w, &requestError{Code: "OperationNotSupported", Text: "WCS is not enabled for this workspace", Status: http.StatusNotFound})
		return
	}
	if !newCapabilityRegistry("2.0.1", ws.Settings.WCS).enabled(extXMLPost) {
		writeException(w, extensionDisabled("request", extXMLPost))
		return
	}
	if h.requireAuth(w, r, ws, ws.Settings.WCS.Public) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/xml" && mediaType != "text/xml") {
		writeException(w, &requestError{Code: "InvalidParameterValue", Locator: "Content-Type", Text: "XML POST requires application/xml or text/xml", Status: http.StatusUnsupportedMediaType})
		return
	}
	limit := maxWCSXMLBody
	if h.cfg.Server.MaxBodyBytes > 0 && h.cfg.Server.MaxBodyBytes < limit {
		limit = h.cfg.Server.MaxBodyBytes
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeException(w, invalid("request", "XML request body is too large or unreadable"))
		return
	}
	values, parseErr := parseXMLRequest(body)
	if parseErr != nil {
		writeException(w, parseErr)
		return
	}
	clone := r.Clone(r.Context())
	copyURL := *r.URL
	copyURL.RawQuery = values.Encode()
	clone.URL = &copyURL
	clone.Method = http.MethodGet
	h.handle(w, clone)
}

func parseXMLRequest(body []byte) (url.Values, *requestError) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, invalid("request", "malformed XML request")
		}
		if _, directive := token.(xml.Directive); directive {
			return nil, invalid("request", "XML directives and DTDs are not allowed")
		}
	}
	var root xmlNode
	decoder = xml.NewDecoder(bytes.NewReader(body))
	decoder.Strict = true
	if err := decoder.Decode(&root); err != nil {
		return nil, invalid("request", "malformed XML request")
	}
	if !validWCSNamespace(root.XMLName.Space) {
		return nil, invalid("request", "unsupported WCS XML namespace")
	}
	operation := root.XMLName.Local
	if operation != "GetCapabilities" && operation != "DescribeCoverage" && operation != "GetCoverage" {
		return nil, &requestError{Code: "OperationNotSupported", Locator: "request", Text: "unsupported WCS XML request"}
	}
	q := url.Values{"REQUEST": []string{operation}}
	for _, attr := range root.Attrs {
		switch strings.ToLower(attr.Name.Local) {
		case "service":
			q.Set("SERVICE", strings.TrimSpace(attr.Value))
		case "version":
			q.Set("VERSION", strings.TrimSpace(attr.Value))
		}
	}
	if operation == "GetCapabilities" {
		if q.Get("SERVICE") == "" {
			q.Set("SERVICE", "WCS")
		}
		for _, version := range descendants(root, "Version") {
			q.Add("ACCEPTVERSIONS", nodeText(version))
		}
		if len(q["ACCEPTVERSIONS"]) > 1 {
			q.Set("ACCEPTVERSIONS", strings.Join(q["ACCEPTVERSIONS"], ","))
		}
		return q, nil
	}
	for _, id := range directChildren(root, "CoverageId") {
		q.Add("COVERAGEID", nodeText(id))
	}
	if operation == "DescribeCoverage" {
		return q, nil
	}
	for _, trim := range directChildren(root, "DimensionTrim") {
		axis, low, high := childText(trim, "Dimension"), childText(trim, "TrimLow"), childText(trim, "TrimHigh")
		if axis == "" || low == "" || high == "" {
			return nil, invalid("subset", "DimensionTrim requires Dimension, TrimLow, and TrimHigh")
		}
		q.Add("SUBSET", fmt.Sprintf("%s(%s,%s)", axis, quoteSubsetCoordinate(low), quoteSubsetCoordinate(high)))
	}
	for _, slice := range directChildren(root, "DimensionSlice") {
		axis, point := childText(slice, "Dimension"), childText(slice, "SlicePoint")
		if axis == "" || point == "" {
			return nil, invalid("subset", "DimensionSlice requires Dimension and SlicePoint")
		}
		q.Add("SUBSET", fmt.Sprintf("%s(%s)", axis, quoteSubsetCoordinate(point)))
	}
	if value := childText(root, "format"); value != "" {
		q.Set("FORMAT", value)
	}
	if value := childText(root, "mediaType"); value != "" {
		q.Set("MEDIATYPE", value)
	}
	for _, extension := range directChildren(root, "Extension") {
		if err := parseXMLExtension(extension, q); err != nil {
			return nil, err
		}
	}
	return q, nil
}

func parseXMLExtension(extension xmlNode, q url.Values) *requestError {
	allowed := map[string]bool{
		"rangesubset": true, "scalebyfactor": true, "scaleaxesbyfactor": true,
		"scaletosize": true, "scaletoextent": true, "subsettingcrs": true,
		"outputcrs": true, "interpolation": true,
	}
	for _, child := range extension.Children {
		name := strings.ToLower(child.XMLName.Local)
		if !allowed[name] {
			return invalid("extension", "unsupported XML extension element "+child.XMLName.Local)
		}
		switch name {
		case "rangesubset":
			var selections []string
			for _, item := range child.Children {
				switch strings.ToLower(item.XMLName.Local) {
				case "rangeitem":
					if component := firstDescendantText(item, "RangeComponent", "rangeComponent"); component != "" {
						selections = append(selections, component)
						continue
					}
					start := firstDescendantText(item, "StartComponent", "startComponent")
					end := firstDescendantText(item, "EndComponent", "endComponent")
					if start == "" || end == "" {
						return invalid("rangeSubset", "invalid XML range item")
					}
					selections = append(selections, start+":"+end)
				case "rangecomponent":
					selections = append(selections, nodeText(item))
				case "rangeinterval":
					start := firstDescendantText(item, "StartComponent", "startComponent")
					end := firstDescendantText(item, "EndComponent", "endComponent")
					if start == "" || end == "" {
						return invalid("rangeSubset", "invalid XML range interval")
					}
					selections = append(selections, start+":"+end)
				}
			}
			if len(selections) == 0 {
				return invalid("rangeSubset", "XML range subset is empty")
			}
			q.Add("RANGESUBSET", strings.Join(selections, ","))
		case "scalebyfactor":
			q.Add("SCALEFACTOR", childText(child, "scaleFactor"))
		case "scaleaxesbyfactor":
			var axes []string
			for _, axis := range directChildren(child, "ScaleAxis") {
				axes = append(axes, fmt.Sprintf("%s(%s)", childText(axis, "axis"), childText(axis, "scaleFactor")))
			}
			q.Add("SCALEAXES", strings.Join(axes, ","))
		case "scaletosize":
			var axes []string
			for _, axis := range directChildren(child, "TargetAxisSize") {
				axes = append(axes, fmt.Sprintf("%s(%s)", childText(axis, "axis"), childText(axis, "targetSize")))
			}
			q.Add("SCALESIZE", strings.Join(axes, ","))
		case "scaletoextent":
			var axes []string
			for _, axis := range directChildren(child, "TargetAxisExtent") {
				axes = append(axes, fmt.Sprintf("%s(%s:%s)", childText(axis, "axis"), childText(axis, "low"), childText(axis, "high")))
			}
			q.Add("SCALEEXTENT", strings.Join(axes, ","))
		case "subsettingcrs":
			q.Add("SUBSETTINGCRS", nodeText(child))
		case "outputcrs":
			q.Add("OUTPUTCRS", nodeText(child))
		case "interpolation":
			if len(descendants(child, "InterpolationAxes")) > 0 {
				return extensionDisabled("interpolation", "interpolation-per-axis")
			}
			q.Add("INTERPOLATION", firstDescendantText(child, "globalInterpolation"))
		}
	}
	return nil
}

func validWCSNamespace(value string) bool {
	return value == "http://www.opengis.net/wcs/2.0" || value == "http://www.opengis.net/wcs/2.1" || value == "http://www.opengis.net/wcs/2.1/gml"
}

func directChildren(node xmlNode, local string) []xmlNode {
	var result []xmlNode
	for _, child := range node.Children {
		if strings.EqualFold(child.XMLName.Local, local) {
			result = append(result, child)
		}
	}
	return result
}

func descendants(node xmlNode, local string) []xmlNode {
	var result []xmlNode
	for _, child := range node.Children {
		if strings.EqualFold(child.XMLName.Local, local) {
			result = append(result, child)
		}
		result = append(result, descendants(child, local)...)
	}
	return result
}

func childText(node xmlNode, local string) string {
	children := directChildren(node, local)
	if len(children) == 0 {
		return ""
	}
	return nodeText(children[0])
}

func firstDescendantText(node xmlNode, names ...string) string {
	for _, name := range names {
		if values := descendants(node, name); len(values) > 0 {
			return nodeText(values[0])
		}
	}
	return ""
}

func nodeText(node xmlNode) string { return strings.TrimSpace(node.Text) }

func quoteSubsetCoordinate(value string) string {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "T:-") && !strings.HasPrefix(value, "-") {
		return `"` + strings.ReplaceAll(value, `"`, "") + `"`
	}
	return value
}
