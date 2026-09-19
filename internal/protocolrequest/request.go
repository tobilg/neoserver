// Package protocolrequest resolves routing semantics once, before authentication,
// authorization and auditing. It deliberately has no dependency on handlers.
package protocolrequest

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Descriptor struct {
	Service          string
	Workspace        string
	Name             string
	ServiceParameter string
	Version          string
	Err              error
}

type contextKey struct{}

func Middleware(basePath string) func(http.Handler) http.Handler {
	basePath = strings.TrimRight(basePath, "/")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Resolve relative to the actual deployment mount, not a coincidental
			// protocol-shaped segment in BasePath (for example /workspaces/x/wms).
			d := resolve(r, "", strings.TrimPrefix(r.URL.Path, basePath))
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, d)))
		})
	}
}

func Prepare(r *http.Request, service string) *http.Request {
	if existing, ok := r.Context().Value(contextKey{}).(Descriptor); ok && (service == "" || existing.Service == service) {
		return r
	}
	d := resolve(r, service, r.URL.Path)
	return r.WithContext(context.WithValue(r.Context(), contextKey{}, d))
}

func Get(r *http.Request) Descriptor {
	prepared := Prepare(r, "")
	return prepared.Context().Value(contextKey{}).(Descriptor)
}

func resolve(r *http.Request, service, path string) Descriptor {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var tail []string
	var workspace string
	for i, part := range parts {
		if part != "workspaces" || i+1 >= len(parts) {
			continue
		}
		workspace = parts[i+1]
		if i+2 >= len(parts) {
			break
		}
		switch parts[i+2] {
		case "wfs", "wms", "wcs", "wmts", "ogc", "ogc-tiles":
			if service == "" || service == parts[i+2] || (service == "ogcapi" && parts[i+2] == "ogc") {
				service, tail = parts[i+2], parts[i+3:]
			}
		}
		break
	}
	if service == "ogc" {
		service = "ogcapi"
	}
	d := Descriptor{Service: service, Workspace: workspace, Name: r.Method}
	if service == "" {
		return d
	}
	if service == "ogcapi" || service == "ogc-tiles" {
		// Match router segments, never substrings in a workspace/collection ID.
		switch {
		case service == "ogcapi" && len(tail) == 4 && tail[0] == "collections" && tail[2] == "items":
			d.Name = "GETITEM"
		case service == "ogcapi" && len(tail) == 3 && tail[0] == "collections" && tail[2] == "items":
			d.Name = "GETFEATURES"
		case len(tail) == 1 && tail[0] == "collections":
			d.Name = "LISTCOLLECTIONS"
		case service == "ogc-tiles" && len(tail) == 7 && tail[0] == "collections" && tail[2] == "tiles":
			d.Name = "GETTILE"
		case service == "ogc-tiles" && len(tail) == 8 && tail[0] == "collections" && tail[2] == "map" && tail[3] == "tiles":
			d.Name = "GETTILE"
		}
		return d // REST operations cannot be overridden by a query parameter.
	}
	if service == "wmts" && len(tail) > 0 {
		switch {
		case len(tail) == 2 && tail[1] == "WMTSCapabilities.xml":
			d.Name = "GETCAPABILITIES"
		case len(tail) == 9:
			d.Name = "GETFEATUREINFO"
		default:
			d.Name = "GETTILE"
		}
		return d
	}
	q, err := queryRouting(r)
	if err != nil {
		d.Err = err
		return d
	}
	d.Name, d.ServiceParameter, d.Version = strings.ToUpper(q["REQUEST"]), strings.ToUpper(q["SERVICE"]), q["VERSION"]
	if r.Method == http.MethodPost && r.Body != nil && (service == "wfs" || service == "wcs") {
		// Server middleware has already installed MaxBytesReader. Restore the
		// body even on an error so downstream code never sees a truncated success.
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			d.Err = errors.New("unreadable or oversized XML request")
			return d
		}
		if len(bytes.TrimSpace(body)) > 0 {
			root, err := xmlRoot(body)
			if err != nil {
				d.Err = err
				return d
			}
			if root.Name.Space != "" && !validNamespace(service, root.Name.Space) {
				d.Err = errors.New("unexpected protocol XML namespace")
				return d
			}
			d.Name = strings.ToUpper(root.Name.Local)
			seen := map[string]bool{}
			for _, attr := range root.Attr {
				name := strings.ToUpper(attr.Name.Local)
				if name != "SERVICE" && name != "VERSION" {
					continue
				}
				if seen[name] || attr.Name.Space != "" {
					d.Err = errors.New("ambiguous XML routing attribute")
					return d
				}
				seen[name] = true
				if name == "SERVICE" {
					d.ServiceParameter = strings.ToUpper(attr.Value)
				} else {
					d.Version = attr.Value
				}
			}
		}
	}
	return d
}

func validNamespace(service, namespace string) bool {
	switch service {
	case "wfs":
		return namespace == "http://www.opengis.net/wfs/2.0" || namespace == "http://www.opengis.net/wfs"
	case "wcs":
		return namespace == "http://www.opengis.net/wcs/2.0" || namespace == "http://www.opengis.net/wcs/2.1"
	}
	return false
}

func queryRouting(r *http.Request) (map[string]string, error) {
	values := map[string]string{}
	for key, items := range r.URL.Query() {
		key = strings.ToUpper(key)
		if key != "REQUEST" && key != "SERVICE" && key != "VERSION" {
			continue
		}
		for _, item := range items {
			if key != "VERSION" {
				item = strings.ToUpper(item)
			}
			if previous, exists := values[key]; exists && previous != item {
				return nil, fmt.Errorf("conflicting %s parameters", key)
			}
			values[key] = item
		}
	}
	return values, nil
}

func xmlRoot(body []byte) (xml.StartElement, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var root xml.StartElement
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return root, errors.New("malformed XML request")
		}
		switch token := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root.Name.Local != "" {
					return root, errors.New("multiple XML request roots")
				}
				root = token
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.Directive:
			return root, errors.New("XML directives are not supported")
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(token)) != 0 {
				return root, errors.New("text outside XML request root")
			}
		}
	}
	if root.Name.Local == "" || depth != 0 {
		return root, errors.New("missing XML request root")
	}
	return root, nil
}

// Action is shared by operation policies and their editor. Stored-query
// administration needs manage, not the ordinary editor's write grant.
func Action(service, operation, method string) string {
	switch strings.ToUpper(operation) {
	case "CREATESTOREDQUERY", "DROPSTOREDQUERY":
		if service == "wfs" {
			return "manage"
		}
	case "TRANSACTION", "LOCKFEATURE", "GETFEATUREWITHLOCK":
		if service == "wfs" {
			return "write"
		}
	case "GETCAPABILITIES", "DESCRIBEFEATURETYPE", "GETFEATURE", "GETPROPERTYVALUE", "LISTSTOREDQUERIES", "DESCRIBESTOREDQUERIES", "GETMAP", "GETFEATUREINFO", "GETLEGENDGRAPHIC", "DESCRIBELAYER", "GETTILE", "GETCOVERAGE", "DESCRIBECOVERAGE", "GETITEM", "GETFEATURES", "LISTCOLLECTIONS", "GET", "HEAD":
		return "read"
	}
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return "read"
	}
	return "write"
}

func (d Descriptor) Mutating(method string) bool { return Action(d.Service, d.Name, method) != "read" }
