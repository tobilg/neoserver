package mgmt

import "encoding/json"

// A managed binding is created by the import publisher, never by a client's
// connection payload. Ordinary updates may retain an existing binding only.
func validManagedBinding(existing, replacement json.RawMessage) bool {
	var before, after struct {
		ImportID string `json:"managed_import_id"`
	}
	if len(existing) != 0 && json.Unmarshal(existing, &before) != nil {
		return false
	}
	if len(replacement) != 0 && json.Unmarshal(replacement, &after) != nil {
		return false
	}
	return before.ImportID == after.ImportID
}
