package wfs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

// StoredQuery is an alias for store.WFSStoredQuery for convenience
type StoredQuery = store.WFSStoredQuery

// StoredQueryParameter is an alias for store.WFSStoredQueryParameter for convenience
type StoredQueryParameter = store.WFSStoredQueryParameter

// XML structures for CreateStoredQuery

// XMLCreateStoredQuery represents a wfs:CreateStoredQuery request
type XMLCreateStoredQuery struct {
	XMLName     xml.Name                   `xml:"CreateStoredQuery"`
	Service     string                     `xml:"service,attr"`
	Version     string                     `xml:"version,attr"`
	Definitions []XMLStoredQueryDefinition `xml:"StoredQueryDefinition"`
}

// XMLStoredQueryDefinition represents a stored query definition
type XMLStoredQueryDefinition struct {
	ID               string                       `xml:"id,attr"`
	Title            []string                     `xml:"Title"`
	Abstract         []string                     `xml:"Abstract"`
	Parameters       []XMLStoredQueryParameterDef `xml:"Parameter"`
	QueryExpressions []XMLQueryExpressionText     `xml:"QueryExpressionText"`
}

// XMLStoredQueryParameterDef represents a parameter definition
type XMLStoredQueryParameterDef struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

// XMLQueryExpressionText represents the query expression
type XMLQueryExpressionText struct {
	Language           string `xml:"language,attr"`
	ReturnFeatureTypes string `xml:"returnFeatureTypes,attr"`
	IsPrivate          string `xml:"isPrivate,attr"`
	Content            string `xml:",innerxml"`
}

// XMLDropStoredQuery represents a wfs:DropStoredQuery request
type XMLDropStoredQuery struct {
	XMLName xml.Name `xml:"DropStoredQuery"`
	Service string   `xml:"service,attr"`
	Version string   `xml:"version,attr"`
	ID      string   `xml:"id,attr"`
}

// Supported query language URNs
const (
	QueryLanguageWFS    = "urn:ogc:def:queryLanguage:OGC-WFS::WFS_QueryExpression"
	QueryLanguageWFSAlt = "urn:ogc:def:queryLanguage:OGC-WFS::WFSQueryExpression" // Alternative without underscore (used by CITE tests)
)

// handleCreateStoredQuery handles WFS CreateStoredQuery requests
func (h *workspaceHandler) handleCreateStoredQuery(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteException(w, ExceptionOperationParsingFailed, "", "Failed to read request body")
		return
	}

	// Parse request
	var req XMLCreateStoredQuery
	if err := xml.Unmarshal(body, &req); err != nil {
		WriteException(w, ExceptionOperationParsingFailed, "", fmt.Sprintf("Failed to parse CreateStoredQuery request: %v", err))
		return
	}

	// Validate service
	if req.Service != "" && strings.ToUpper(req.Service) != "WFS" {
		WriteException(w, ExceptionInvalidParameterValue, "service", "SERVICE must be WFS")
		return
	}

	// Process each stored query definition
	for _, def := range req.Definitions {
		// Validate query ID
		if def.ID == "" {
			WriteException(w, ExceptionMissingParameterValue, "id", "StoredQueryDefinition requires an id attribute")
			return
		}

		// Check if ID conflicts with built-in GetFeatureById
		if IsGetFeatureByIdQuery(def.ID) {
			WriteException(w, ExceptionInvalidParameterValue, "id", fmt.Sprintf("Cannot override built-in stored query: %s", def.ID))
			return
		}

		// Check for duplicate
		if existing, _ := h.store.GetWFSStoredQuery(ctx, ws.ID, def.ID); existing != nil {
			WriteException(w, ExceptionDuplicateStoredQueryIdValue, def.ID, fmt.Sprintf("Stored query with id '%s' already exists", def.ID))
			return
		}

		// Validate query expressions
		if len(def.QueryExpressions) == 0 {
			WriteException(w, ExceptionMissingParameterValue, "QueryExpressionText", "StoredQueryDefinition requires at least one QueryExpressionText")
			return
		}

		// Validate language (accept both URN variants)
		for _, expr := range def.QueryExpressions {
			if expr.Language != "" && expr.Language != QueryLanguageWFS && expr.Language != QueryLanguageWFSAlt {
				WriteException(w, ExceptionInvalidParameterValue, "language", fmt.Sprintf("Unsupported query language: %s. Supported: %s", expr.Language, QueryLanguageWFS))
				return
			}
		}

		// Build input for store
		input := store.CreateWFSStoredQueryInput{
			WorkspaceID: ws.ID,
			QueryID:     def.ID,
		}

		if len(def.Title) > 0 {
			input.Title = def.Title[0]
		}
		if len(def.Abstract) > 0 {
			input.Abstract = def.Abstract[0]
		}

		for _, param := range def.Parameters {
			input.Parameters = append(input.Parameters, store.WFSStoredQueryParameter{
				Name: param.Name,
				Type: param.Type,
			})
		}

		if len(def.QueryExpressions) > 0 {
			input.Language = def.QueryExpressions[0].Language
			input.QueryExpression = def.QueryExpressions[0].Content
			if def.QueryExpressions[0].ReturnFeatureTypes != "" {
				input.ReturnTypes = strings.Fields(def.QueryExpressions[0].ReturnFeatureTypes)
			}
		}

		// Add to store
		if _, err := h.store.CreateWFSStoredQuery(ctx, input); err != nil {
			if err == store.ErrDuplicateKey {
				WriteException(w, ExceptionDuplicateStoredQueryIdValue, input.QueryID, fmt.Sprintf("Stored query with id '%s' already exists", input.QueryID))
			} else {
				h.writeInternalError(w, "Failed to create stored query", err)
			}
			return
		}
	}

	// Write success response
	WriteCreateStoredQueryResponse(w)
}

// handleDropStoredQuery handles WFS DropStoredQuery requests
func (h *workspaceHandler) handleDropStoredQuery(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()
	q := NormalizeQuery(r)

	// Get stored query ID from query params or XML body
	storedQueryID := q.Get("ID")

	// Check for XML POST body
	if r.Method == http.MethodPost {
		body, err := GetBodyBytes(r)
		if err == nil && len(body) > 0 {
			var req XMLDropStoredQuery
			if err := xml.Unmarshal(body, &req); err == nil {
				if req.ID != "" {
					storedQueryID = req.ID
				}
			}
		}
	}

	// Validate ID
	if storedQueryID == "" {
		WriteException(w, ExceptionMissingParameterValue, "id", "DropStoredQuery requires an id parameter")
		return
	}

	// Cannot drop built-in GetFeatureById
	if IsGetFeatureByIdQuery(storedQueryID) {
		WriteException(w, ExceptionInvalidParameterValue, "id", fmt.Sprintf("Cannot drop built-in stored query: %s", storedQueryID))
		return
	}

	// Remove from store
	if err := h.store.DeleteWFSStoredQuery(ctx, ws.ID, storedQueryID); err != nil {
		if err == store.ErrNotFound {
			WriteException(w, ExceptionInvalidParameterValue, "id", fmt.Sprintf("stored query with id '%s' not found", storedQueryID))
		} else {
			h.writeInternalError(w, "Failed to delete stored query", err)
		}
		return
	}

	// Write success response
	WriteDropStoredQueryResponse(w)
}

// WriteCreateStoredQueryResponse writes a successful CreateStoredQuery response
func WriteCreateStoredQueryResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:CreateStoredQueryResponse xmlns:wfs="%s"/>`, NSWfs)
}

// WriteDropStoredQueryResponse writes a successful DropStoredQuery response
func WriteDropStoredQueryResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:DropStoredQueryResponse xmlns:wfs="%s" status="OK"/>`, NSWfs)
}
