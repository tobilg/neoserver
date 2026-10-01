// Package staccatalog owns the encrypted, workspace-partitioned STAC inventory.
package staccatalog

import (
	"errors"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"time"
)

var ErrNotFound = errors.New("STAC resource not found")
var ErrConflict = errors.New("STAC resource changed or already exists")

type Collection struct {
	Document     stacmodel.Document `json:"document"`
	Public       bool               `json:"public"`
	AllowedRoles []string           `json:"allowed_roles,omitempty"`
	Binding      *Binding           `json:"binding,omitempty"`
	Revision     int64              `json:"revision"`
	ItemCount    int64              `json:"item_count"`
	UpdatedAt    time.Time          `json:"updated_at"`
	// SourceFingerprint identifies the source state of the published generation.
	SourceFingerprint string `json:"-"`
}
type Value struct {
	Property string `json:"property,omitempty"`
	Constant any    `json:"constant,omitempty"`
}
type AssetMapping struct {
	Href  Value    `json:"href"`
	Type  string   `json:"type,omitempty"`
	Title string   `json:"title,omitempty"`
	Roles []string `json:"roles,omitempty"`
}
type Mapping struct {
	ID         string                  `json:"id_property,omitempty"`
	Datetime   Value                   `json:"datetime,omitempty"`
	Start      Value                   `json:"start_datetime,omitempty"`
	End        Value                   `json:"end_datetime,omitempty"`
	Assets     map[string]AssetMapping `json:"assets,omitempty"`
	Properties map[string]Value        `json:"properties,omitempty"`
	Extensions []string                `json:"extensions,omitempty"`
}
type Binding struct {
	ID                 string  `json:"id"`
	ServiceID          string  `json:"service_id"`
	ResourceID         string  `json:"resource_id"`
	ResourceKind       string  `json:"resource_kind"`
	Mode               string  `json:"mode"`
	Mapping            Mapping `json:"mapping"`
	Filter             string  `json:"filter,omitempty"`
	RefreshIntervalSec int     `json:"refresh_interval_sec"`
}
type Job struct {
	ID           string             `json:"id"`
	WorkspaceID  string             `json:"workspace_id"`
	CollectionID string             `json:"collection_id"`
	Kind         string             `json:"kind"`
	Status       string             `json:"status"`
	Revision     int64              `json:"revision"`
	Processed    int64              `json:"processed"`
	Error        string             `json:"error,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Request      stacmodel.Document `json:"request,omitempty"`
}
type LocalAsset struct {
	CollectionID string `json:"collection_id"`
	ItemID       string `json:"item_id,omitempty"`
	Key          string `json:"key"`
	Path         string `json:"path"`
	MediaType    string `json:"media_type"`
}
type Search struct {
	Collections []string           `json:"collections,omitempty"`
	IDs         []string           `json:"ids,omitempty"`
	BBox        []float64          `json:"bbox,omitempty"`
	Datetime    string             `json:"datetime,omitempty"`
	Intersects  stacmodel.Document `json:"intersects,omitempty"`
	Limit       int                `json:"limit,omitempty"`
	Token       string             `json:"token,omitempty"`
}
type Page struct {
	Items []stacmodel.Document
	Next  string
}
