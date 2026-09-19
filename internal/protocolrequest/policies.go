package protocolrequest

import (
	"fmt"
	"net/http"
)

// PolicyOperations is also exported in OpenAPI for the console's policy editor.
// Names are the canonical operations emitted by the request resolver.
func PolicyOperations() map[string]map[string]string {
	services := map[string][]string{
		"wfs":       {"GETCAPABILITIES", "DESCRIBEFEATURETYPE", "GETFEATURE", "GETPROPERTYVALUE", "LISTSTOREDQUERIES", "DESCRIBESTOREDQUERIES", "TRANSACTION", "LOCKFEATURE", "GETFEATUREWITHLOCK", "CREATESTOREDQUERY", "DROPSTOREDQUERY"},
		"wms":       {"GETCAPABILITIES", "GETMAP", "GETFEATUREINFO", "GETLEGENDGRAPHIC", "DESCRIBELAYER"},
		"wcs":       {"GETCAPABILITIES", "DESCRIBECOVERAGE", "GETCOVERAGE"},
		"wmts":      {"GETCAPABILITIES", "GETTILE", "GETFEATUREINFO"},
		"ogcapi":    {"GET", "HEAD", "GETITEM", "GETFEATURES", "LISTCOLLECTIONS"},
		"ogc-tiles": {"GET", "HEAD", "GETTILE", "LISTCOLLECTIONS"},
	}
	result := make(map[string]map[string]string, len(services))
	for service, operations := range services {
		result[service] = make(map[string]string, len(operations))
		for _, operation := range operations {
			result[service][operation] = Action(service, operation, http.MethodGet)
		}
	}
	return result
}

func ValidatePolicy(service, operation, action string) error {
	operations := PolicyOperations()[service]
	if operation == "" || operation == "*" {
		for _, supported := range operations {
			if action == supported {
				return nil
			}
		}
		return fmt.Errorf("action %q is not supported by service %q", action, service)
	}
	expected, ok := operations[operation]
	if !ok {
		return fmt.Errorf("operation %q is not supported by service %q", operation, service)
	}
	if action != expected {
		return fmt.Errorf("%s requires action %q, not %q", operation, expected, action)
	}
	return nil
}
