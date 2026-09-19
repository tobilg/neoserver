package wfs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// parseXMLWithNamespaces parses XML and extracts namespace bindings to resolve type names.
// It returns a map of namespace URI to our known prefix (e.g., "http://cite.opengeospatial.org/gmlsf" -> "cite").
func parseXMLWithNamespaces(body []byte) map[string]string {
	nsMap := make(map[string]string)
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		if startElem, ok := token.(xml.StartElement); ok {
			for _, attr := range startElem.Attr {
				// Check for xmlns declarations
				if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
					uri := attr.Value
					// Map the URI to our known prefix
					switch uri {
					case NSCite:
						nsMap[attr.Name.Local] = "cite"
					case NSDefault:
						nsMap[attr.Name.Local] = "app"
					case NSWfs:
						nsMap[attr.Name.Local] = "wfs"
					case NSGml:
						nsMap[attr.Name.Local] = "gml"
					}
				}
			}
		}
	}
	return nsMap
}

// resolveTypeName resolves a type name like "ns30:Bridges" to "cite:Bridges" using namespace bindings.
func resolveTypeName(typeName string, nsMap map[string]string) string {
	if idx := strings.Index(typeName, ":"); idx >= 0 {
		prefix := typeName[:idx]
		localPart := typeName[idx+1:]
		// Check if this prefix maps to a known namespace
		if knownPrefix, ok := nsMap[prefix]; ok {
			return knownPrefix + ":" + localPart
		}
	}
	return typeName
}

// An absent filter is distinct from an invalid supplied filter. HTTP decoders
// resolve inherited namespace bindings before saving inner XML; the wrapper
// also supports legacy programmatic fragments with conventional prefixes.
func extractFilterFromXML(rawXML string) (string, error) {
	d := xml.NewDecoder(strings.NewReader(`<root xmlns:fes="` + NSFes + `" xmlns:wfs="` + NSWfs + `" xmlns:gml="` + NSGml + `">` + rawXML + `</root>`))
	var result string
	for {
		token, err := d.Token()
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return "", &RequestError{Code: ExceptionOperationParsingFailed, Locator: "Filter", Message: err.Error()}
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "root" || start.Name.Local == "Query" {
			continue
		}
		if start.Name.Local != "Filter" {
			if strings.EqualFold(start.Name.Local, "Filter") {
				return "", &RequestError{Code: ExceptionOperationParsingFailed, Locator: "Filter", Message: "Filter element names are case-sensitive"}
			}
			if err := d.Skip(); err != nil {
				return "", err
			}
			continue
		}
		if result != "" || (start.Name.Space != "" && start.Name.Space != NSFes) {
			return "", &RequestError{Code: ExceptionOperationParsingFailed, Locator: "Filter", Message: "expected one FES Filter element"}
		}
		inner, err := resolvedInnerXML(d, start)
		if err != nil {
			return "", err
		}
		result = `<Filter xmlns="` + NSFes + `">` + inner + `</Filter>`
		if _, err := ParseFESFilter(result); err != nil {
			return "", &RequestError{Code: ExceptionOperationParsingFailed, Locator: "Filter", Message: fmt.Sprint(err)}
		}
	}
}

// ParseGetFeatureRequest parses a GetFeature request from query parameters or XML POST body.
func ParseGetFeatureRequest(r *http.Request, maxFeatures, defaultCount int) (*GetFeatureRequest, error) {
	q := NormalizeQuery(r)

	req := &GetFeatureRequest{
		Version:      q.Get("VERSION"),
		OutputFormat: q.Get("OUTPUTFORMAT"),
		ResultType:   strings.ToLower(q.Get("RESULTTYPE")),
		SrsName:      q.Get("SRSNAME"),
		Filter:       q.Get("FILTER"),
		Resolve:      strings.ToLower(q.Get("RESOLVE")),
		ResolveDepth: q.Get("RESOLVEDEPTH"),
	}

	// Check for XML POST body
	if r.Method == http.MethodPost {
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "xml") || contentType == "" {
			body, err := GetBodyBytes(r)
			if err == nil && len(body) > 0 {
				// Parse namespace bindings to resolve dynamic prefixes (e.g., ns30:Bridges -> cite:Bridges)
				nsMap := parseXMLWithNamespaces(body)

				var xmlReq XMLGetFeature
				if err := xml.Unmarshal(body, &xmlReq); err == nil {
					// Override with XML values
					if xmlReq.Version != "" {
						req.Version = xmlReq.Version
					}
					if xmlReq.OutputFormat != "" {
						req.OutputFormat = xmlReq.OutputFormat
					}
					if xmlReq.ResultType != "" {
						req.ResultType = strings.ToLower(xmlReq.ResultType)
					}
					if xmlReq.Count != "" {
						if count, err := strconv.Atoi(xmlReq.Count); err == nil {
							req.Count = count
						}
					}
					if xmlReq.StartIndex != "" {
						if startIndex, err := strconv.Atoi(xmlReq.StartIndex); err == nil {
							req.StartIndex = startIndex
						}
					}
					// Extract type names from Query elements
					for _, query := range xmlReq.Queries {
						if len(query.PropertyNames) > 0 {
							req.PropertyName = append(req.PropertyName, query.PropertyNames...)
						}
						if query.TypeNames != "" {
							// Parse and resolve each type name using namespace bindings
							rawNames := parseTypeNames(query.TypeNames)
							for _, name := range rawNames {
								resolved := resolveTypeName(name, nsMap)
								req.TypeNames = append(req.TypeNames, resolved)
							}
						}
						if query.SrsName != "" {
							req.SrsName = query.SrsName
						}
						sort, err := extractSortFromXML(query.FilterRaw)
						if err != nil {
							return nil, err
						}
						if sort != "" {
							q.Set("SORTBY", sort)
						}
						// Extract filter from Query's raw inner XML
						if query.FilterRaw != "" {
							// The FilterRaw contains the inner XML of the Query element
							// which may include a Filter element - extract and use it directly
							filterXML, err := extractFilterFromXML(query.FilterRaw)
							if err != nil {
								return nil, err
							}
							if filterXML != "" {
								req.Filter = filterXML
							}
						}
					}

					// Handle StoredQuery from XML
					if xmlReq.StoredQuery != nil {
						req.StoredQueryID = xmlReq.StoredQuery.ID
						req.StoredQueryParams = make(map[string]string)
						for _, param := range xmlReq.StoredQuery.Parameters {
							paramValue := strings.TrimSpace(param.Value)
							// If the parameter value looks like a QName (has a colon),
							// resolve it using the namespace map
							if strings.Contains(paramValue, ":") {
								paramValue = resolveTypeName(paramValue, nsMap)
							}
							req.StoredQueryParams[strings.ToUpper(param.Name)] = paramValue
						}
					}
				}
			}
		}
	}

	// Default output format
	if req.OutputFormat == "" {
		req.OutputFormat = FormatGML32
	}
	if !isSupportedGetFeatureOutput(req.OutputFormat) {
		return nil, &RequestError{
			Code: ExceptionInvalidParameterValue, Locator: "outputFormat",
			Message: "Unsupported outputFormat: " + req.OutputFormat,
		}
	}

	// Default result type
	if req.ResultType == "" {
		req.ResultType = ResultTypeResults
	}

	// Validate result type
	if req.ResultType != ResultTypeResults && req.ResultType != ResultTypeHits {
		return nil, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "resultType",
			Message: "Invalid resultType: " + req.ResultType,
		}
	}

	// Parse TYPENAMES from query params (required unless using stored query or XML body)
	typeNames := q.Get("TYPENAMES")
	if typeNames == "" {
		typeNames = q.Get("TYPENAME") // WFS 1.x compatibility
	}
	if typeNames != "" {
		// Check for NAMESPACES parameter to resolve dynamic prefixes
		namespacesParam := q.Get("NAMESPACES")
		nsMap := parseNamespacesParam(namespacesParam)

		rawNames := parseTypeNames(typeNames)
		for _, name := range rawNames {
			resolved := resolveTypeName(name, nsMap)
			req.TypeNames = append(req.TypeNames, resolved)
		}
	}

	// Parse SRSNAME
	if req.SrsName != "" {
		srid, err := ParseSRSName(req.SrsName)
		if err != nil {
			return nil, &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "srsName",
				Message: err.Error(),
			}
		}
		req.SRID = srid
	}

	// Parse COUNT from GET parameter (only if not already set from XML body)
	if countStr := q.Get("COUNT"); countStr != "" {
		count, err := strconv.Atoi(countStr)
		if err != nil || count < 0 {
			return nil, &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "count",
				Message: "COUNT must be a non-negative integer",
			}
		}
		if count > maxFeatures {
			count = maxFeatures
		}
		req.Count = count
	} else if req.Count == 0 {
		// Only apply default if neither XML body nor GET parameter set a count
		req.Count = defaultCount
	}

	// Ensure count doesn't exceed max
	if req.Count > maxFeatures {
		req.Count = maxFeatures
	}

	// Parse STARTINDEX from GET parameter (only if not already set from XML body)
	// (StartIndex is already handled above, this check is for GET-only requests)
	if startIndexStr := q.Get("STARTINDEX"); startIndexStr != "" && req.StartIndex == 0 {
		startIndex, err := strconv.Atoi(startIndexStr)
		if err != nil || startIndex < 0 {
			return nil, &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "startIndex",
				Message: "STARTINDEX must be a non-negative integer",
			}
		}
		req.StartIndex = startIndex
	}

	// Parse BBOX
	if bboxStr := q.Get("BBOX"); bboxStr != "" {
		bbox, bboxSRID, err := parseBBox(bboxStr)
		if err != nil {
			return nil, &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "bbox",
				Message: err.Error(),
			}
		}
		req.BBox = bbox
		req.BBoxSRID = bboxSRID
	}

	// Parse SORTBY
	if sortByStr := q.Get("SORTBY"); sortByStr != "" {
		sortBy, err := parseSortBy(sortByStr)
		if err != nil {
			return nil, &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "sortBy",
				Message: err.Error(),
			}
		}
		req.SortBy = sortBy
	}

	// Parse PROPERTYNAME
	if propNameStr := q.Get("PROPERTYNAME"); propNameStr != "" {
		req.PropertyName = strings.Split(propNameStr, ",")
	}

	// Parse RESOURCEID (WFS 2.0 feature ID filter)
	if resourceID := q.Get("RESOURCEID"); resourceID != "" {
		req.ResourceID = strings.Split(resourceID, ",")
	}

	// Parse STOREDQUERY_ID from GET parameters (WFS 2.0 uses underscore per Table 14 in spec)
	// Only if not already set from XML body
	if req.StoredQueryID == "" {
		storedQueryID := q.Get("STOREDQUERY_ID")
		if storedQueryID == "" {
			storedQueryID = q.Get("STOREDQUERYID") // Also try without underscore for compatibility
		}
		if storedQueryID != "" {
			req.StoredQueryID = storedQueryID
			req.StoredQueryParams = make(map[string]string)
			nsMap := parseNamespacesParam(q.Get("NAMESPACES"))

			// Standard WFS parameters that should NOT be treated as stored query parameters
			// TYPENAME(S) are intentionally not reserved here: stored-query parameter
			// names are definition-specific, and the standard CITE query uses typeName.
			standardParams := map[string]bool{
				"SERVICE": true, "VERSION": true, "REQUEST": true,
				"COUNT": true, "MAXFEATURES": true, "STARTINDEX": true,
				"BBOX": true, "FILTER": true, "SORTBY": true,
				"OUTPUTFORMAT": true, "SRSNAME": true,
				"PROPERTYNAME": true, "RESOURCEID": true,
				"STOREDQUERY_ID": true, "STOREDQUERYID": true,
				"NAMESPACES": true, "RESULTTYPE": true,
				"RESOLVE": true, "RESOLVEPATH": true, "RESOLVETIMEOUT": true, "RESOLVEDEPTH": true,
			}

			// Capture all non-standard query parameters as stored query parameters
			// WFS 2.0 spec: stored query parameters are passed as regular query params
			for key, values := range r.URL.Query() {
				upperKey := strings.ToUpper(key)
				if !standardParams[upperKey] && len(values) > 0 {
					paramValue := values[0]
					if strings.Contains(paramValue, ":") {
						paramValue = resolveTypeName(paramValue, nsMap)
					}
					req.StoredQueryParams[upperKey] = paramValue
				}
			}
		}
	}

	return req, nil
}

// ParseDescribeFeatureTypeRequest parses a DescribeFeatureType request.
func ParseDescribeFeatureTypeRequest(r *http.Request) (*DescribeFeatureTypeRequest, error) {
	q := NormalizeQuery(r)

	req := &DescribeFeatureTypeRequest{
		Version:      q.Get("VERSION"),
		OutputFormat: q.Get("OUTPUTFORMAT"),
	}

	// Check for XML POST body
	if r.Method == http.MethodPost {
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "xml") || contentType == "" {
			body, err := GetBodyBytes(r)
			if err == nil && len(body) > 0 {
				// Parse namespace bindings for type name resolution
				nsMap := parseXMLWithNamespaces(body)

				var xmlReq XMLDescribeFeatureType
				if err := xml.Unmarshal(body, &xmlReq); err == nil {
					if xmlReq.Version != "" {
						req.Version = xmlReq.Version
					}
					if xmlReq.OutputFormat != "" {
						req.OutputFormat = xmlReq.OutputFormat
					}
					// Resolve type names using namespace bindings
					for _, typeName := range xmlReq.TypeNames {
						resolved := resolveTypeName(typeName, nsMap)
						req.TypeNames = append(req.TypeNames, resolved)
					}
				}
			}
		}
	}

	// Default output format
	if req.OutputFormat == "" {
		req.OutputFormat = FormatXMLSubtype
	}
	if !isSupportedDescribeFeatureTypeOutput(req.OutputFormat) {
		return nil, &RequestError{
			Code: ExceptionInvalidParameterValue, Locator: "outputFormat",
			Message: "Unsupported outputFormat: " + req.OutputFormat,
		}
	}

	// Parse TYPENAMES from GET parameters (if not already set by POST body)
	if len(req.TypeNames) == 0 {
		typeNames := q.Get("TYPENAMES")
		if typeNames == "" {
			typeNames = q.Get("TYPENAME")
		}
		if typeNames != "" {
			// Check for NAMESPACES parameter to resolve dynamic prefixes
			namespacesParam := q.Get("NAMESPACES")
			nsMap := parseNamespacesParam(namespacesParam)

			rawNames := parseTypeNames(typeNames)
			for _, name := range rawNames {
				resolved := resolveTypeName(name, nsMap)
				req.TypeNames = append(req.TypeNames, resolved)
			}
		}
	}

	return req, nil
}

// ParseDescribeStoredQueriesRequest parses a DescribeStoredQueries request.
func ParseDescribeStoredQueriesRequest(r *http.Request) (*DescribeStoredQueriesRequest, error) {
	q := NormalizeQuery(r)

	req := &DescribeStoredQueriesRequest{
		Version: q.Get("VERSION"),
	}

	// Check for XML POST body
	if r.Method == http.MethodPost {
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "xml") || contentType == "" {
			body, err := GetBodyBytes(r)
			if err == nil && len(body) > 0 {
				var xmlReq XMLDescribeStoredQueries
				if err := xml.Unmarshal(body, &xmlReq); err == nil {
					if xmlReq.Version != "" {
						req.Version = xmlReq.Version
					}
					req.StoredQueryIds = xmlReq.StoredQueryIds
				}
			}
		}
	}

	// Parse STOREDQUERY_ID from GET parameters (if not already set by POST body)
	// WFS 2.0 uses STOREDQUERY_ID (with underscore) per Table 23 in the spec
	if len(req.StoredQueryIds) == 0 {
		// Try both variants for compatibility: STOREDQUERY_ID (standard) and STOREDQUERYID
		storedQueryID := q.Get("STOREDQUERY_ID")
		if storedQueryID == "" {
			storedQueryID = q.Get("STOREDQUERYID")
		}
		if storedQueryID != "" {
			req.StoredQueryIds = []string{storedQueryID}
		}
	}

	return req, nil
}

// ParseGetPropertyValueRequest parses a GetPropertyValue request.
func ParseGetPropertyValueRequest(r *http.Request, maxFeatures, defaultCount int) (*GetPropertyValueRequest, error) {
	q := NormalizeQuery(r)
	_, present := q["VALUEREFERENCE"]
	value := q.Get("VALUEREFERENCE")
	if r.Method == http.MethodPost && (strings.Contains(r.Header.Get("Content-Type"), "xml") || r.Header.Get("Content-Type") == "") {
		body, err := GetBodyBytes(r)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 {
			var envelope XMLGetPropertyValue
			if err := xml.Unmarshal(body, &envelope); err != nil {
				return nil, &RequestError{Code: ExceptionOperationParsingFailed, Locator: "GetPropertyValue", Message: "Invalid GetPropertyValue XML"}
			}
			value, present = envelope.ValueReference, true
			for name, attribute := range map[string]string{"VERSION": envelope.Version, "COUNT": envelope.Count, "STARTINDEX": envelope.StartIndex, "RESULTTYPE": envelope.ResultType} {
				if attribute != "" {
					q.Set(name, attribute)
				}
			}
			var names []string
			bindings := parseXMLWithNamespaces(body)
			for _, query := range envelope.Queries {
				for _, name := range parseTypeNames(query.TypeNames) {
					names = append(names, resolveTypeName(name, bindings))
				}
				if query.SrsName != "" {
					q.Set("SRSNAME", query.SrsName)
				}
				filter, err := extractFilterFromXML(query.FilterRaw)
				if err != nil {
					return nil, err
				}
				if filter != "" {
					q.Set("FILTER", filter)
				}
				sort, err := extractSortFromXML(query.FilterRaw)
				if err != nil {
					return nil, err
				}
				if sort != "" {
					q.Set("SORTBY", sort)
				}
			}
			if len(names) > 0 {
				q.Set("TYPENAMES", strings.Join(names, ","))
			}
		}
	}
	if value == "" {
		if !present {
			return nil, &RequestError{Code: ExceptionMissingParameterValue, Locator: "valueReference", Message: "VALUEREFERENCE parameter is required"}
		}
		return nil, &RequestError{Code: ExceptionInvalidParameterValue, Locator: "valueReference", Message: "VALUEREFERENCE parameter cannot be empty"}
	}
	// Parse the normalized effective query once using the feature-query rules:
	// counts, offsets, filters, sorting, namespaces, CRS and resource IDs.
	normalized := r.Clone(r.Context())
	requestURL := *r.URL
	requestURL.RawQuery = q.Encode()
	normalized.URL, normalized.Method = &requestURL, http.MethodGet
	effective, err := ParseGetFeatureRequest(normalized, maxFeatures, defaultCount)
	if err != nil {
		return nil, err
	}
	if effective.StoredQueryID != "" {
		return nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "storedQueryId", Message: "Stored queries are not supported for GetPropertyValue"}
	}
	return &GetPropertyValueRequest{Version: effective.Version, TypeNames: effective.TypeNames, ValueReference: value, SrsName: effective.SrsName, SRID: effective.SRID, BBox: effective.BBox, BBoxSRID: effective.BBoxSRID, Filter: effective.Filter, Count: effective.Count, StartIndex: effective.StartIndex, ResultType: effective.ResultType, SortBy: effective.SortBy, Resolve: effective.Resolve, ResolveDepth: effective.ResolveDepth, ResourceID: effective.ResourceID}, nil
}

// Query inner XML has resolved namespaces; match SortBy by local element name
// and apply the same order validation used for key/value requests.
func extractSortFromXML(raw string) (string, error) {
	var root struct {
		SortBy []struct {
			Properties []struct {
				Value string `xml:"ValueReference"`
				Order string `xml:"SortOrder"`
			} `xml:"SortProperty"`
		} `xml:"SortBy"`
	}
	if err := xml.Unmarshal([]byte("<root>"+raw+"</root>"), &root); err != nil {
		return "", err
	}
	if len(root.SortBy) > 1 {
		return "", &RequestError{Code: ExceptionInvalidParameterValue, Locator: "sortBy", Message: "Only one SortBy element is supported"}
	}
	var parts []string
	for _, sort := range root.SortBy {
		for _, prop := range sort.Properties {
			order := strings.ToUpper(strings.TrimSpace(prop.Order))
			switch order {
			case "", "ASC":
				order = "A"
			case "DESC":
				order = "D"
			default:
				return "", &RequestError{Code: ExceptionInvalidParameterValue, Locator: "sortBy", Message: "SortOrder must be ASC or DESC"}
			}
			parts = append(parts, strings.TrimSpace(prop.Value)+" "+order)
		}
	}
	return strings.Join(parts, ","), nil
}
