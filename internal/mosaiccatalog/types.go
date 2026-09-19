// Package mosaiccatalog owns the optional operational index for managed
// raster mosaics. Its DuckDB database is deliberately separate from both the
// neoserver catalog and the persistent tile-cache index.
package mosaiccatalog

import (
	"errors"
	"time"
)

var (
	ErrDisabled         = errors.New("mosaic catalog is disabled")
	ErrNotFound         = errors.New("mosaic catalog record not found")
	ErrServiceNotMosaic = errors.New("service is not a raster mosaic")
)

type Granule struct {
	ID               string     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	ServiceID        string     `json:"service_id"`
	Generation       int64      `json:"generation"`
	SourceURI        string     `json:"source_uri"`
	CRS              string     `json:"crs"`
	SRID             int        `json:"srid,omitempty"`
	BBox             [4]float64 `json:"bbox"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
	BandCount        int        `json:"band_count"`
	DataType         string     `json:"data_type"`
	ResolutionX      float64    `json:"resolution_x"`
	ResolutionY      float64    `json:"resolution_y"`
	Time             string     `json:"time,omitempty"`
	Elevation        *float64   `json:"elevation,omitempty"`
	Priority         int        `json:"priority,omitempty"`
	SizeBytes        int64      `json:"size_bytes,omitempty"`
	ModifiedAt       *time.Time `json:"modified_at,omitempty"`
	FootprintWKB     []byte     `json:"-"`
	FootprintGeoJSON any        `json:"footprint,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type GranuleFilter struct {
	BBox      *[4]float64
	Time      string
	Elevation *float64
	Limit     int
	Offset    int
}

type HarvestMode string

const (
	HarvestAppend      HarvestMode = "append"
	HarvestSynchronize HarvestMode = "synchronize"
)

type HarvestGranule struct {
	Path      string   `json:"path"`
	Time      string   `json:"time,omitempty"`
	Elevation *float64 `json:"elevation,omitempty"`
	Priority  int      `json:"priority,omitempty"`
}

type HarvestRequest struct {
	Mode      HarvestMode      `json:"mode,omitempty"`
	Directory string           `json:"directory,omitempty"`
	Pattern   string           `json:"pattern,omitempty"`
	Granules  []HarvestGranule `json:"granules,omitempty"`
}

type JobStatus string

const (
	JobQueued     JobStatus = "queued"
	JobRunning    JobStatus = "running"
	JobCancelling JobStatus = "cancelling"
	JobCancelled  JobStatus = "cancelled"
	JobSucceeded  JobStatus = "succeeded"
	JobFailed     JobStatus = "failed"
)

type Job struct {
	ID              string         `json:"id"`
	WorkspaceID     string         `json:"workspace_id"`
	ServiceID       string         `json:"service_id"`
	Request         HarvestRequest `json:"request"`
	Status          JobStatus      `json:"status"`
	TotalGranules   int64          `json:"total_granules"`
	Processed       int64          `json:"processed_granules"`
	Succeeded       int64          `json:"succeeded_granules"`
	Failed          int64          `json:"failed_granules"`
	Generation      int64          `json:"generation,omitempty"`
	CancelRequested bool           `json:"cancel_requested"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	CreatedBy       string         `json:"created_by,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	CompletedAt     *time.Time     `json:"completed_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
}
