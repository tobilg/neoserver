package wfs

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

// WFS Transaction XML structures

// WFSTransaction represents a wfs:Transaction request
type WFSTransaction struct {
	XMLName        xml.Name           `xml:"Transaction"`
	Version        string             `xml:"version,attr"`
	Service        string             `xml:"service,attr"`
	Handle         string             `xml:"handle,attr,omitempty"`
	LockId         string             `xml:"lockId,attr,omitempty"`
	ReleaseAction  string             `xml:"releaseAction,attr,omitempty"` // ALL or SOME
	SrsName        string             `xml:"srsName,attr,omitempty"`
	Inserts        []WFSInsert        `xml:"Insert"`
	Updates        []WFSUpdate        `xml:"Update"`
	Deletes        []WFSDelete        `xml:"Delete"`
	Replaces       []WFSReplace       `xml:"Replace"`
	NativeElements []WFSNativeElement `xml:"Native"`
	operations     []transactionOperation
}

// WFSInsert represents a wfs:Insert element
type WFSInsert struct {
	Handle      string       `xml:"handle,attr,omitempty"`
	InputFormat string       `xml:"inputFormat,attr,omitempty"`
	SrsName     string       `xml:"srsName,attr,omitempty"`
	Features    []XMLFeature `xml:",any"` // Raw feature elements
}

// WFSUpdate represents a wfs:Update element
type WFSUpdate struct {
	Handle     string        `xml:"handle,attr,omitempty"`
	TypeName   string        `xml:"typeName,attr"`
	SrsName    string        `xml:"srsName,attr,omitempty"`
	Properties []WFSProperty `xml:"Property"`
	FilterRaw  string        `xml:",innerxml"` // Capture raw inner XML for filter extraction
}

// WFSProperty represents a wfs:Property element in an Update
type WFSProperty struct {
	ValueReference WFSValueReference `xml:"ValueReference"`
	Value          *WFSValue         `xml:"Value"`
}

// WFSValueReference represents the property name reference
type WFSValueReference struct {
	Value  string `xml:",chardata"`
	Action string `xml:"action,attr,omitempty"` // insertBefore, insertAfter, remove, replace
}

// WFSValue represents the property value
type WFSValue struct {
	RawXML string `xml:",innerxml"`
	Nil    string `xml:"http://www.w3.org/2001/XMLSchema-instance nil,attr"`
}

// WFSDelete represents a wfs:Delete element
type WFSDelete struct {
	Handle    string `xml:"handle,attr,omitempty"`
	TypeName  string `xml:"typeName,attr"`
	FilterRaw string `xml:",innerxml"` // Capture raw inner XML for filter extraction
}

// WFSReplace represents a wfs:Replace element
type WFSReplace struct {
	Handle   string `xml:"handle,attr,omitempty"`
	SrsName  string `xml:"srsName,attr,omitempty"`
	InnerXML string `xml:",innerxml"` // Capture raw inner XML for feature and filter extraction
}

// WFSNativeElement represents a wfs:Native element for vendor-specific operations
type WFSNativeElement struct {
	VendorId     string `xml:"vendorId,attr"`
	SafeToIgnore bool   `xml:"safeToIgnore,attr"`
	Content      string `xml:",innerxml"`
}

// XMLFeature represents a GML feature element
type XMLFeature struct {
	XMLName    xml.Name
	GmlId      string `xml:"id,attr"`
	Properties string `xml:",innerxml"` // Raw property XML
}

// TransactionResponse represents the response to a Transaction request
type TransactionResponse struct {
	XMLName            xml.Name           `xml:"wfs:TransactionResponse"`
	Version            string             `xml:"version,attr"`
	XmlnsWfs           string             `xml:"xmlns:wfs,attr"`
	XmlnsFes           string             `xml:"xmlns:fes,attr,omitempty"`
	TransactionSummary TransactionSummary `xml:"wfs:TransactionSummary"`
	InsertResults      *InsertResults     `xml:"wfs:InsertResults,omitempty"`
	UpdateResults      *UpdateResults     `xml:"wfs:UpdateResults,omitempty"`
	ReplaceResults     *ReplaceResults    `xml:"wfs:ReplaceResults,omitempty"`

	// versionEvents carries the committed mutations so version metadata is
	// recorded only after the whole transaction succeeded.
	versionEvents []versionEvent `xml:"-"`
}

// TransactionSummary contains counts of the operations performed
type TransactionSummary struct {
	TotalInserted int `xml:"wfs:totalInserted,omitempty"`
	TotalUpdated  int `xml:"wfs:totalUpdated,omitempty"`
	TotalReplaced int `xml:"wfs:totalReplaced,omitempty"`
	TotalDeleted  int `xml:"wfs:totalDeleted,omitempty"`
}

// versionEvent captures one committed feature mutation so version metadata
// can be recorded after the transaction succeeds. Recording must never happen
// for a transaction that failed and rolled back.
type versionEvent struct {
	action    string // "insert" | "update" | "delete"
	layerName string
	featureID string
}

const (
	versionActionInsert = "insert"
	versionActionUpdate = "update"
	versionActionDelete = "delete"
)

// InsertResults contains the IDs of inserted features
type InsertResults struct {
	Features []Feature `xml:"wfs:Feature"`
}

// UpdateResults contains the IDs of updated features
type UpdateResults struct {
	Features []Feature `xml:"wfs:Feature"`
}

// ReplaceResults contains the IDs of replaced features
type ReplaceResults struct {
	Features []Feature `xml:"wfs:Feature"`
}

// Feature represents a feature reference in transaction results
type Feature struct {
	Handle     string       `xml:"handle,attr,omitempty"`
	ResourceId []ResourceId `xml:"fes:ResourceId"`
}

// ResourceId represents a feature ID in transaction results
type ResourceId struct {
	Rid string `xml:"rid,attr"`
}

// ParseTransactionRequest parses a WFS Transaction XML request
func ParseTransactionRequest(body []byte) (*WFSTransaction, error) {
	var tx WFSTransaction
	if err := xml.Unmarshal(body, &tx); err != nil {
		return nil, &RequestError{
			Code:    ExceptionOperationParsingFailed,
			Locator: "Transaction",
			Message: fmt.Sprintf("Failed to parse Transaction request: %v", err),
		}
	}

	// Validate service
	if tx.Service != "" && strings.ToUpper(tx.Service) != "WFS" {
		return nil, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "service",
			Message: "SERVICE must be WFS",
		}
	}

	// Validate version
	if tx.Version != "" && tx.Version != "2.0.0" && tx.Version != "2.0.2" {
		return nil, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "version",
			Message: fmt.Sprintf("Unsupported version: %s", tx.Version),
		}
	}

	return &tx, nil
}

// handleTransaction handles WFS Transaction requests
func (h *workspaceHandler) handleTransaction(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Check if transactional WFS is enabled
	// For now, we'll check if the workspace has any writable services
	// In a production system, this would be controlled by configuration

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteException(w, ExceptionOperationParsingFailed, "", "Failed to read request body")
		return
	}

	// Parse transaction request
	tx, err := ParseTransactionRequest(body)
	if err != nil {
		if reqErr, ok := err.(*RequestError); ok {
			WriteException(w, reqErr.Code, reqErr.Locator, reqErr.Message)
		} else {
			WriteException(w, ExceptionOperationParsingFailed, "", err.Error())
		}
		return
	}
	if tx.LockId != "" {
		if err := h.state.Locks.ValidateLockOwner(tx.LockId, ws.ID, lockOwner(r)); err != nil {
			WriteExceptionFromError(w, err)
			return
		}
	}

	// Execute transaction
	if h.registry != nil {
		finish, err := h.registry.BeginDataWrite(ctx, ws.ID)
		if err != nil {
			h.writeInternalError(w, "could not establish transaction cache barrier", err)
			return
		}
		defer func() {
			if err := finish(); err != nil {
				h.logger.Error("feature transaction cache barrier remains pending", "workspace", ws.ID, "error", err)
			}
		}()
	}
	result, err := h.executeTransaction(ctx, ws, tx)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}
	// The transaction is committed; record feature version metadata. Failed
	// transactions never reach this point, so nothing is recorded for them.
	h.recordVersionEvents(ws.ID, result.versionEvents, lockOwner(r))
	if tx.LockId != "" && (tx.ReleaseAction == "" || strings.EqualFold(tx.ReleaseAction, "ALL")) {
		if err := h.state.Locks.ReleaseLockOwned(tx.LockId, lockOwner(r)); err != nil {
			WriteException(w, ExceptionInvalidParameterValue, "lockId", fmt.Sprintf("Failed to release lock: %s", tx.LockId))
			return
		}
	}
	if h.cache != nil {
		h.cache.InvalidateWorkspace(ws.ID)
	}

	// Write response
	WriteTransactionResponse(w, result)
}

// recordVersionEvents records feature version metadata for a committed
// transaction. Versioning is metadata-only bookkeeping, so a nil runtime
// state simply skips recording.
func (h *workspaceHandler) recordVersionEvents(workspaceID string, events []versionEvent, modifiedBy string) {
	if h.state == nil || h.state.Versions == nil {
		return
	}
	h.state.Versions.recordEvents(workspaceID, events, modifiedBy)
}

// executeTransaction executes all operations in a transaction
func (h *workspaceHandler) executeTransaction(ctx context.Context, ws *workspace.Workspace, tx *WFSTransaction) (*TransactionResponse, error) {
	if h.state != nil && h.state.Locks != nil {
		release, err := h.state.Locks.writes.acquire(ctx, ws.ID)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	service, err := h.transactionService(ws, tx)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return h.executeTransactionWithWriter(ctx, ws, tx, nil)
	}
	atomic, ok := service.DataSource.(datasource.AtomicWritableDataSource)
	if !ok {
		return nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "Transaction", Message: "data source does not support atomic transactions"}
	}
	var response *TransactionResponse
	err = atomic.AtomicWrite(ctx, func(writer datasource.FeatureWriter) error {
		var runErr error
		response, runErr = h.executeTransactionWithWriter(ctx, ws, tx, writer)
		return runErr
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (h *workspaceHandler) transactionService(ws *workspace.Workspace, tx *WFSTransaction) (*workspace.Service, error) {
	var selected *workspace.Service
	check := func(layerName, invalidCode string) error {
		layer, service := resolveFeatureLayer(ws, layerName, h.cfg.WFS.AppNamespacePrefix)
		if layer == nil || service == nil {
			return &RequestError{Code: invalidCode, Locator: "Transaction", Message: fmt.Sprintf("Unknown feature type: %s", layerName)}
		}
		if layer.IsSQLView || (layer.SQLViewConfig != nil && layer.SQLViewConfig.ReadOnly) {
			return &RequestError{Code: ExceptionOperationNotSupported, Locator: "Transaction", Message: "SQL view publications are read-only"}
		}
		if selected != nil && selected.ID != service.ID {
			return &RequestError{Code: ExceptionOperationNotSupported, Locator: "Transaction", Message: "a transaction may reference only one service"}
		}
		selected = service
		return nil
	}
	for _, insert := range tx.Inserts {
		for _, feature := range insert.Features {
			if err := check(feature.XMLName.Local, ExceptionInvalidValue); err != nil {
				return nil, err
			}
		}
	}
	for _, update := range tx.Updates {
		if err := check(update.TypeName, ExceptionInvalidParameterValue); err != nil {
			return nil, err
		}
	}
	for _, deleteOp := range tx.Deletes {
		if err := check(deleteOp.TypeName, ExceptionInvalidParameterValue); err != nil {
			return nil, err
		}
	}
	for _, replace := range tx.Replaces {
		feature, err := extractFeatureFromReplaceXML(replace.InnerXML)
		if err != nil {
			return nil, err
		}
		if err := check(feature.XMLName.Local, ExceptionInvalidParameterValue); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func (h *workspaceHandler) executeTransactionWithWriter(ctx context.Context, ws *workspace.Workspace, tx *WFSTransaction, writer datasource.FeatureWriter) (*TransactionResponse, error) {
	responseVersion := "2.0.0"
	if tx.Version == "2.0.2" {
		responseVersion = tx.Version
	}
	response := &TransactionResponse{Version: responseVersion, XmlnsWfs: NSWfs, XmlnsFes: NSFes}
	var events []versionEvent
	for _, op := range tx.orderedOperations() {
		switch op.kind {
		case "Insert":
			v := tx.Inserts[op.index]
			if v.SrsName == "" {
				v.SrsName = tx.SrsName
			}
			ids, err := h.processInsert(ctx, ws, writer, &v, &events)
			if err != nil {
				return nil, err
			}
			response.TransactionSummary.TotalInserted += len(ids)
			if len(ids) > 0 {
				if response.InsertResults == nil {
					response.InsertResults = &InsertResults{}
				}
				response.InsertResults.Features = append(response.InsertResults.Features, Feature{Handle: v.Handle, ResourceId: ids})
			}
		case "Update":
			v := tx.Updates[op.index]
			if v.SrsName == "" {
				v.SrsName = tx.SrsName
			}
			count, ids, err := h.processUpdate(ctx, ws, writer, &v, tx.LockId, &events)
			if err != nil {
				return nil, err
			}
			response.TransactionSummary.TotalUpdated += count
			if len(ids) > 0 {
				if response.UpdateResults == nil {
					response.UpdateResults = &UpdateResults{}
				}
				response.UpdateResults.Features = append(response.UpdateResults.Features, Feature{Handle: v.Handle, ResourceId: ids})
			}
		case "Delete":
			v := tx.Deletes[op.index]
			count, err := h.processDelete(ctx, ws, writer, &v, tx.LockId, &events)
			if err != nil {
				return nil, err
			}
			response.TransactionSummary.TotalDeleted += count
		case "Replace":
			v := tx.Replaces[op.index]
			if v.SrsName == "" {
				v.SrsName = tx.SrsName
			}
			ids, err := h.processReplace(ctx, ws, writer, &v, tx.LockId, &events)
			if err != nil {
				return nil, err
			}
			response.TransactionSummary.TotalReplaced += len(ids)
			if len(ids) > 0 {
				if response.ReplaceResults == nil {
					response.ReplaceResults = &ReplaceResults{}
				}
				response.ReplaceResults.Features = append(response.ReplaceResults.Features, Feature{Handle: v.Handle, ResourceId: ids})
			}
		}
	}
	response.versionEvents = events
	return response, nil
}

// processInsert processes a single Insert operation
func (h *workspaceHandler) processInsert(ctx context.Context, ws *workspace.Workspace, writer datasource.FeatureWriter, insert *WFSInsert, events *[]versionEvent) ([]ResourceId, error) {
	var results []ResourceId

	for _, feature := range insert.Features {
		// Determine the layer from the feature type name
		layerName := feature.XMLName.Local
		if feature.XMLName.Space != "" {
			// Strip namespace prefix if present
			layerName = feature.XMLName.Local
		}

		layer, service := resolveFeatureLayer(ws, layerName, h.cfg.WFS.AppNamespacePrefix)
		if layer == nil || service == nil {
			return nil, &RequestError{
				Code:    ExceptionInvalidValue,
				Locator: "Insert",
				Message: fmt.Sprintf("Unknown feature type: %s", layerName),
			}
		}
		layerName = ParseQName(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)).LocalPart

		// Check if the data source supports writes
		if writer == nil {
			return nil, &RequestError{
				Code:    ExceptionOperationNotSupported,
				Locator: "Insert",
				Message: fmt.Sprintf("Layer %s does not support insert operations", layerName),
			}
		}

		// Parse feature properties from XML
		props, geom, err := parseFeatureProperties(feature.Properties)
		if err != nil {
			return nil, &RequestError{
				Code:    ExceptionOperationParsingFailed,
				Locator: "Insert",
				Message: fmt.Sprintf("Failed to parse feature properties: %v", err),
			}
		}

		var geometry datasource.GeometryValue
		if geom != "" {
			geometry, err = h.transactionGeometry(geom, insert.SrsName)
			if err != nil {
				return nil, &RequestError{Code: ExceptionInvalidValue, Locator: "srsName", Message: err.Error()}
			}
		}
		if returning, ok := writer.(datasource.ReturningFeatureWriter); ok {
			info, err := returning.GetLayerInfo(ctx, layer.SourceLayer)
			if err != nil {
				return nil, err
			}
			props, err = nativeTransactionProperties(info, props)
			if err != nil {
				return nil, err
			}
		}

		// Create the feature
		featureData := datasource.FeatureData{
			ID:           feature.GmlId,
			Properties:   props,
			Geometry:     geometry.GML,
			GeometrySRID: geometry.SRID,
		}

		// Insert the feature
		ids, err := writer.Insert(ctx, layer.SourceLayer, []datasource.FeatureData{featureData})
		if err != nil {
			return nil, &RequestError{
				Code:    ExceptionNoApplicableCode,
				Locator: "Insert",
				Message: fmt.Sprintf("Insert failed: %v", err),
			}
		}

		for _, id := range ids {
			results = append(results, ResourceId{
				Rid: fmt.Sprintf("%s.%s", layerName, id),
			})
			*events = append(*events, versionEvent{action: versionActionInsert, layerName: layerName, featureID: fmt.Sprintf("%v", id)})
		}
	}

	return results, nil
}

// processUpdate processes a single Update operation
func (h *workspaceHandler) processUpdate(ctx context.Context, ws *workspace.Workspace, writer datasource.FeatureWriter, update *WFSUpdate, lockId string, events *[]versionEvent) (int, []ResourceId, error) {
	layer, service := resolveFeatureLayer(ws, update.TypeName, h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil {
		return 0, nil, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "Update",
			Message: fmt.Sprintf("Unknown feature type: %s", update.TypeName),
		}
	}
	layerName := ParseQName(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)).LocalPart

	// Check if the data source supports writes
	if writer == nil {
		return 0, nil, &RequestError{
			Code:    ExceptionOperationNotSupported,
			Locator: "Update",
			Message: fmt.Sprintf("Layer %s does not support update operations", layerName),
		}
	}

	// Get layer info for filter compilation
	layerInfo, err := transactionLayerInfo(ctx, service, writer, layer.SourceLayer)
	if err != nil {
		return 0, nil, &RequestError{
			Code:    ExceptionNoApplicableCode,
			Locator: "Update",
			Message: fmt.Sprintf("Failed to get layer info: %v", err),
		}
	}

	// Extract and compile filter
	filterXML, err := extractFilterFromXML(update.FilterRaw)
	if err != nil {
		return 0, nil, err
	}
	var filterSQL string
	var filterArgs []interface{}

	if filterXML != "" {
		fesFilter, err := ParseFESFilter(filterXML)
		if err != nil {
			return 0, nil, err
		}

		// Build allowed properties set
		allowed := make(map[string]struct{})
		for _, prop := range layerInfo.Properties {
			allowed[prop.Name] = struct{}{}
		}

		sql, args, _, err := CompileFES(fesFilter, FESCompileOptions{
			StartParamIndex:   1,
			SourceSRID:        layerInfo.SRID,
			GeometryProperty:  layerInfo.GeometryColumn,
			AllowedProperties: allowed,
			CollectionID:      layerName,
			IDColumn:          layerInfo.IDColumn,
		})
		if err != nil {
			return 0, nil, err
		}
		filterSQL = sql
		filterArgs = args
	}

	// Build allowed properties set for validation
	allowedProps := make(map[string]struct{})
	for _, prop := range layerInfo.Properties {
		allowedProps[prop.Name] = struct{}{}
	}

	if layerInfo.GeometryColumn != "" {
		allowedProps[layerInfo.GeometryColumn] = struct{}{}
	}
	xmlNames, err := propertyXMLNames(layerInfo)
	if err != nil {
		return 0, nil, err
	}

	// Build properties map with validation
	properties := make(map[string]interface{})
	for _, prop := range update.Properties {
		propName := strings.TrimSpace(prop.ValueReference.Value)
		originalPropName := propName // Keep original for error messages

		// Strip XPath index notation (e.g., gml:name[1] -> gml:name)
		if idx := strings.Index(propName, "["); idx >= 0 {
			propName = propName[:idx]
		}

		// Check for reserved GML properties that cannot be updated (gml:boundedBy, gml:id, etc.)
		// These are identified by the gml: namespace prefix
		if strings.HasPrefix(strings.ToLower(propName), "gml:") {
			localName := propName[4:] // Remove "gml:" prefix
			if isReservedGMLProperty(localName) {
				return 0, nil, &RequestError{
					Code:    ExceptionInvalidValue,
					Locator: originalPropName,
					Message: fmt.Sprintf("Cannot update reserved GML property: %s", originalPropName),
				}
			}
			// For gml:name and gml:description, map to layer property
			propName = localName
		}

		// Strip namespace prefix for property lookup
		if idx := strings.Index(propName, ":"); idx >= 0 {
			propName = propName[idx+1:]
		}

		for native, advertised := range xmlNames {
			if propName == advertised {
				propName = native
				break
			}
		}
		if propName == layerInfo.IDColumn {
			return 0, nil, &RequestError{Code: ExceptionInvalidValue, Locator: originalPropName, Message: "feature identifiers are immutable"}
		}

		// Validate that the property exists in the layer
		if _, exists := allowedProps[propName]; !exists {
			return 0, nil, &RequestError{
				Code:    ExceptionInvalidValue,
				Locator: originalPropName,
				Message: fmt.Sprintf("Unknown property: %s", originalPropName),
			}
		}

		var value interface{}
		var valueErr error
		if propName == layerInfo.GeometryColumn && prop.Value != nil && prop.Value.Nil != "true" && prop.Value.Nil != "1" {
			value, valueErr = h.transactionGeometry(prop.Value.RawXML, update.SrsName)
		} else {
			value, valueErr = scalarXML(prop.Value)
			if valueErr == nil {
				value, valueErr = typedScalar(layerInfo, propName, value)
			}
		}
		if valueErr != nil {
			return 0, nil, &RequestError{Code: ExceptionInvalidValue, Locator: originalPropName, Message: valueErr.Error()}
		}
		properties[propName] = value
	}

	returning, ok := writer.(datasource.ReturningFeatureWriter)
	if !ok {
		return 0, nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "Update", Message: "atomic affected-feature reporting is required"}
	}
	ids, err := returning.UpdateReturning(ctx, layer.SourceLayer, properties, filterSQL, filterArgs)
	if err != nil {
		return 0, nil, &RequestError{Code: ExceptionNoApplicableCode, Locator: "Update", Message: fmt.Sprintf("Update failed: %v", err)}
	}
	if err := h.checkMutationLocks(ws, layerName, lockId, ids); err != nil {
		return 0, nil, err
	}
	affected := make([]ResourceId, 0, len(ids))
	for _, id := range ids {
		affected = append(affected, ResourceId{Rid: layerName + "." + id})
		*events = append(*events, versionEvent{action: versionActionUpdate, layerName: layerName, featureID: id})
	}
	return len(ids), affected, nil
}

// processDelete processes a single Delete operation
func (h *workspaceHandler) processDelete(ctx context.Context, ws *workspace.Workspace, writer datasource.FeatureWriter, delete *WFSDelete, lockId string, events *[]versionEvent) (int, error) {
	layer, service := resolveFeatureLayer(ws, delete.TypeName, h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil {
		return 0, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "Delete",
			Message: fmt.Sprintf("Unknown feature type: %s", delete.TypeName),
		}
	}
	layerName := ParseQName(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)).LocalPart

	// Check if the data source supports writes
	if writer == nil {
		return 0, &RequestError{
			Code:    ExceptionOperationNotSupported,
			Locator: "Delete",
			Message: fmt.Sprintf("Layer %s does not support delete operations", layerName),
		}
	}

	// Get layer info for filter compilation
	layerInfo, err := transactionLayerInfo(ctx, service, writer, layer.SourceLayer)
	if err != nil {
		return 0, &RequestError{
			Code:    ExceptionNoApplicableCode,
			Locator: "Delete",
			Message: fmt.Sprintf("Failed to get layer info: %v", err),
		}
	}

	// Extract and compile filter
	filterXML, err := extractFilterFromXML(delete.FilterRaw)
	if err != nil {
		return 0, err
	}
	if filterXML == "" {
		return 0, &RequestError{
			Code:    ExceptionMissingParameterValue,
			Locator: "Delete",
			Message: "Delete requires a Filter element",
		}
	}

	fesFilter, err := ParseFESFilter(filterXML)
	if err != nil {
		return 0, err
	}

	// Build allowed properties set
	allowed := make(map[string]struct{})
	for _, prop := range layerInfo.Properties {
		allowed[prop.Name] = struct{}{}
	}

	filterSQL, filterArgs, _, err := CompileFES(fesFilter, FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        layerInfo.SRID,
		GeometryProperty:  layerInfo.GeometryColumn,
		AllowedProperties: allowed,
		CollectionID:      layerName,
		IDColumn:          layerInfo.IDColumn,
	})
	if err != nil {
		return 0, err
	}

	returning, ok := writer.(datasource.ReturningFeatureWriter)
	if !ok {
		return 0, &RequestError{Code: ExceptionOperationNotSupported, Locator: "Delete", Message: "atomic affected-feature reporting is required"}
	}
	ids, err := returning.DeleteReturning(ctx, layer.SourceLayer, filterSQL, filterArgs)
	if err != nil {
		return 0, &RequestError{Code: ExceptionNoApplicableCode, Locator: "Delete", Message: fmt.Sprintf("Delete failed: %v", err)}
	}
	if err := h.checkMutationLocks(ws, layerName, lockId, ids); err != nil {
		return 0, err
	}
	for _, id := range ids {
		*events = append(*events, versionEvent{action: versionActionDelete, layerName: layerName, featureID: id})
	}
	return len(ids), nil
}

// processReplace processes a single Replace operation
func (h *workspaceHandler) processReplace(ctx context.Context, ws *workspace.Workspace, writer datasource.FeatureWriter, replace *WFSReplace, lockId string, events *[]versionEvent) ([]ResourceId, error) {
	// Extract feature from inner XML (first non-Filter element)
	feature, err := extractFeatureFromReplaceXML(replace.InnerXML)
	if err != nil {
		return nil, &RequestError{
			Code:    ExceptionOperationParsingFailed,
			Locator: "Replace",
			Message: fmt.Sprintf("Failed to parse replacement feature: %v", err),
		}
	}

	// Determine the layer from the feature type name
	layerName := feature.XMLName.Local

	layer, service := resolveFeatureLayer(ws, layerName, h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil {
		return nil, &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "Replace",
			Message: fmt.Sprintf("Unknown feature type: %s", layerName),
		}
	}
	layerName = ParseQName(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)).LocalPart

	// Check if the data source supports writes
	if writer == nil {
		return nil, &RequestError{
			Code:    ExceptionOperationNotSupported,
			Locator: "Replace",
			Message: fmt.Sprintf("Layer %s does not support replace operations", layerName),
		}
	}

	// Get layer info for filter compilation
	layerInfo, err := transactionLayerInfo(ctx, service, writer, layer.SourceLayer)
	if err != nil {
		return nil, &RequestError{
			Code:    ExceptionNoApplicableCode,
			Locator: "Replace",
			Message: fmt.Sprintf("Failed to get layer info: %v", err),
		}
	}

	// Extract and compile filter to find the feature to replace
	filterXML, err := extractFilterFromXML(replace.InnerXML)
	if err != nil {
		return nil, err
	}
	if filterXML == "" {
		return nil, &RequestError{
			Code:    ExceptionMissingParameterValue,
			Locator: "Replace",
			Message: "Replace requires a Filter element",
		}
	}

	fesFilter, err := ParseFESFilter(filterXML)
	if err != nil {
		return nil, err
	}

	// Build allowed properties set
	allowed := make(map[string]struct{})
	for _, prop := range layerInfo.Properties {
		allowed[prop.Name] = struct{}{}
	}

	filterSQL, filterArgs, _, err := CompileFES(fesFilter, FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        layerInfo.SRID,
		GeometryProperty:  layerInfo.GeometryColumn,
		AllowedProperties: allowed,
		CollectionID:      layerName,
		IDColumn:          layerInfo.IDColumn,
	})
	if err != nil {
		return nil, err
	}

	if layerInfo.IDColumn == "" {
		return nil, &RequestError{Code: ExceptionOperationNotSupported, Locator: "Replace", Message: "a stable feature identifier is required"}
	}

	props, geom, err := parseFeatureProperties(feature.Properties)
	if err != nil {
		return nil, &RequestError{
			Code:    ExceptionOperationParsingFailed,
			Locator: "Replace",
			Message: fmt.Sprintf("Failed to parse feature properties: %v", err),
		}
	}

	var geometry datasource.GeometryValue
	if geom != "" {
		geometry, err = h.transactionGeometry(geom, replace.SrsName)
		if err != nil {
			return nil, &RequestError{Code: ExceptionInvalidValue, Locator: "srsName", Message: err.Error()}
		}
	}
	if returning, ok := writer.(datasource.ReturningFeatureWriter); ok {
		info, err := returning.GetLayerInfo(ctx, layer.SourceLayer)
		if err != nil {
			return nil, err
		}
		props, err = nativeTransactionProperties(info, props)
		if err != nil {
			return nil, err
		}
	}

	featureData := datasource.FeatureData{
		ID:           feature.GmlId,
		Properties:   props,
		Geometry:     geometry.GML,
		GeometrySRID: geometry.SRID,
	}

	// Execute replace
	ids, err := writer.Replace(ctx, layer.SourceLayer, featureData, filterSQL, filterArgs)
	if err != nil {
		return nil, &RequestError{
			Code:    ExceptionNoApplicableCode,
			Locator: "Replace",
			Message: fmt.Sprintf("Replace failed: %v", err),
		}
	}

	if err := h.checkMutationLocks(ws, layerName, lockId, ids); err != nil {
		return nil, err
	}

	var results []ResourceId
	for _, id := range ids {
		results = append(results, ResourceId{
			Rid: fmt.Sprintf("%s.%s", layerName, id),
		})
		// Replace supersedes the previous version, same as an update.
		*events = append(*events, versionEvent{action: versionActionUpdate, layerName: layerName, featureID: fmt.Sprintf("%v", id)})
	}

	return results, nil
}

// parseFeatureProperties parses feature properties from XML
func parseFeatureProperties(propsXML string) (map[string]interface{}, string, error) {
	return parseTransactionProperties(propsXML)
}

// isGeometryElement checks if an element name is a GML geometry type
func isGeometryElement(name string) bool {
	geomTypes := []string{
		"Point", "LineString", "Polygon", "MultiPoint", "MultiLineString",
		"MultiPolygon", "MultiCurve", "MultiSurface", "MultiGeometry", "GeometryCollection", "Envelope", "LinearRing",
	}
	for _, t := range geomTypes {
		if strings.EqualFold(name, t) {
			return true
		}
	}
	return false
}

// isReservedGMLProperty checks if a property name is a reserved GML property
// that cannot be updated directly via WFS Transaction (when prefixed with gml:)
func isReservedGMLProperty(name string) bool {
	// Only truly reserved GML properties that cannot be updated:
	// - boundedBy: computed bounding box, not a user-editable property
	// - id: feature identifier, should not be changed via Update
	reservedProps := []string{
		"boundedBy", // gml:boundedBy - feature bounding box (computed)
		"id",        // gml:id - feature identifier
	}
	for _, p := range reservedProps {
		if strings.EqualFold(name, p) {
			return true
		}
	}
	return false
}

// extractFeatureFromReplaceXML extracts the feature element from Replace inner XML.
// The Replace element contains both a feature element and a Filter element.
// This function returns the first non-Filter element as the feature.
func extractFeatureFromReplaceXML(innerXML string) (*XMLFeature, error) {
	d := xml.NewDecoder(strings.NewReader(transactionNamespaces + innerXML + "</root>"))
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	for {
		token, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("no replacement feature: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "Filter" {
			if err := d.Skip(); err != nil {
				return nil, err
			}
			continue
		}
		var feature XMLFeature
		if err := d.DecodeElement(&feature, &start); err != nil {
			return nil, err
		}
		return &feature, nil
	}
}

// getNamespacePrefix returns a short prefix for known namespaces
func getNamespacePrefix(ns string) string {
	switch ns {
	case "http://www.opengis.net/gml/3.2":
		return "gml"
	case "http://www.opengis.net/fes/2.0":
		return "fes"
	case "http://www.w3.org/2001/XMLSchema-instance":
		return "xsi"
	default:
		return ""
	}
}

// encodeElement recursively encodes an XML element to a string (for geometry)
// It converts namespace URIs to short prefixes and adds namespace declarations
// to the root element for ST_GeomFromGML compatibility.
func encodeElement(w *strings.Builder, decoder *xml.Decoder, start xml.StartElement) error {
	// Write start tag with namespace prefix
	w.WriteString("<")
	prefix := getNamespacePrefix(start.Name.Space)
	if prefix != "" {
		w.WriteString(prefix)
		w.WriteString(":")
	}
	w.WriteString(start.Name.Local)

	// Add namespace declaration on root element for GML (required by ST_GeomFromGML)
	if prefix == "gml" {
		w.WriteString(` xmlns:gml="http://www.opengis.net/gml/3.2"`)
	}

	for _, attr := range start.Attr {
		w.WriteString(" ")
		attrPrefix := getNamespacePrefix(attr.Name.Space)
		if attrPrefix != "" {
			w.WriteString(attrPrefix)
			w.WriteString(":")
		}
		w.WriteString(attr.Name.Local)
		w.WriteString("=\"")
		w.WriteString(attr.Value)
		w.WriteString("\"")
	}
	w.WriteString(">")

	// Process contents
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			w.WriteString("<")
			elemPrefix := getNamespacePrefix(t.Name.Space)
			if elemPrefix != "" {
				w.WriteString(elemPrefix)
				w.WriteString(":")
			}
			w.WriteString(t.Name.Local)
			for _, attr := range t.Attr {
				w.WriteString(" ")
				attrPrefix := getNamespacePrefix(attr.Name.Space)
				if attrPrefix != "" {
					w.WriteString(attrPrefix)
					w.WriteString(":")
				}
				w.WriteString(attr.Name.Local)
				w.WriteString("=\"")
				w.WriteString(escapeXML(attr.Value))
				w.WriteString("\"")
			}
			w.WriteString(">")
		case xml.EndElement:
			depth--
			w.WriteString("</")
			elemPrefix := getNamespacePrefix(t.Name.Space)
			if elemPrefix != "" {
				w.WriteString(elemPrefix)
				w.WriteString(":")
			}
			w.WriteString(t.Name.Local)
			w.WriteString(">")
		case xml.CharData:
			w.Write(t)
		}
	}

	return nil
}

// WriteTransactionResponse writes a WFS Transaction response
func WriteTransactionResponse(w http.ResponseWriter, response *TransactionResponse) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:TransactionResponse version="%s" xmlns:wfs="%s" xmlns:fes="%s">
  <wfs:TransactionSummary>
`, transactionResponseVersion(response.Version), NSWfs, NSFes)

	if response.TransactionSummary.TotalInserted > 0 {
		fmt.Fprintf(w, "    <wfs:totalInserted>%d</wfs:totalInserted>\n", response.TransactionSummary.TotalInserted)
	}
	if response.TransactionSummary.TotalUpdated > 0 {
		fmt.Fprintf(w, "    <wfs:totalUpdated>%d</wfs:totalUpdated>\n", response.TransactionSummary.TotalUpdated)
	}
	if response.TransactionSummary.TotalReplaced > 0 {
		fmt.Fprintf(w, "    <wfs:totalReplaced>%d</wfs:totalReplaced>\n", response.TransactionSummary.TotalReplaced)
	}
	if response.TransactionSummary.TotalDeleted > 0 {
		fmt.Fprintf(w, "    <wfs:totalDeleted>%d</wfs:totalDeleted>\n", response.TransactionSummary.TotalDeleted)
	}

	fmt.Fprintf(w, "  </wfs:TransactionSummary>\n")

	// Write insert results
	if response.InsertResults != nil && len(response.InsertResults.Features) > 0 {
		fmt.Fprintf(w, "  <wfs:InsertResults>\n")
		for _, feature := range response.InsertResults.Features {
			fmt.Fprintf(w, "    <wfs:Feature")
			if feature.Handle != "" {
				fmt.Fprintf(w, ` handle="%s"`, escapeXML(feature.Handle))
			}
			fmt.Fprintf(w, ">\n")
			for _, rid := range feature.ResourceId {
				fmt.Fprintf(w, `      <fes:ResourceId rid="%s"/>`+"\n", escapeXML(rid.Rid))
			}
			fmt.Fprintf(w, "    </wfs:Feature>\n")
		}
		fmt.Fprintf(w, "  </wfs:InsertResults>\n")
	}

	// Write update results
	if response.UpdateResults != nil && len(response.UpdateResults.Features) > 0 {
		fmt.Fprintf(w, "  <wfs:UpdateResults>\n")
		for _, feature := range response.UpdateResults.Features {
			fmt.Fprintf(w, "    <wfs:Feature>\n")
			for _, rid := range feature.ResourceId {
				fmt.Fprintf(w, `      <fes:ResourceId rid="%s"/>`+"\n", escapeXML(rid.Rid))
			}
			fmt.Fprintf(w, "    </wfs:Feature>\n")
		}
		fmt.Fprintf(w, "  </wfs:UpdateResults>\n")
	}

	// Write replace results
	if response.ReplaceResults != nil && len(response.ReplaceResults.Features) > 0 {
		fmt.Fprintf(w, "  <wfs:ReplaceResults>\n")
		for _, feature := range response.ReplaceResults.Features {
			fmt.Fprintf(w, "    <wfs:Feature>\n")
			for _, rid := range feature.ResourceId {
				fmt.Fprintf(w, `      <fes:ResourceId rid="%s"/>`+"\n", escapeXML(rid.Rid))
			}
			fmt.Fprintf(w, "    </wfs:Feature>\n")
		}
		fmt.Fprintf(w, "  </wfs:ReplaceResults>\n")
	}

	fmt.Fprintf(w, "</wfs:TransactionResponse>")
}

// Transaction responses retain the requested supported service version.
func transactionResponseVersion(version string) string {
	if version == "2.0.2" {
		return version
	}
	return "2.0.0"
}
