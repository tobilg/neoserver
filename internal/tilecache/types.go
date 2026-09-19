package tilecache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrBlobNotFound = errors.New("tile blob not found")

const cacheLayoutVersion = "v1"

// Identity is the protocol-neutral identity of one canonical tile.
type Identity struct {
	WorkspaceID       string
	WorkspaceRevision int64
	ResourceID        string
	ResourceKind      string
	Generation        int64
	TileType          string
	MatrixSet         string
	Zoom              int
	Column            int
	Row               int
	StyleDigest       string
	StyleName         string // logical selector, independent of render fingerprints
	Format            string
}

func (i Identity) Validate() error {
	if i.WorkspaceID == "" || i.WorkspaceRevision <= 0 || i.ResourceID == "" || i.Generation <= 0 || i.MatrixSet == "" || i.Zoom < 0 || i.Column < 0 || i.Row < 0 {
		return errors.New("incomplete tile identity")
	}
	if i.TileType != "map" && i.TileType != "vector" {
		return fmt.Errorf("invalid tile type %q", i.TileType)
	}
	if extensionForFormat(i.Format) == "" {
		return fmt.Errorf("unsupported tile format %q", i.Format)
	}
	return nil
}

func (i Identity) CanonicalKey() string {
	return strings.Join([]string{
		cacheLayoutVersion, i.WorkspaceID, fmt.Sprintf("%d", i.WorkspaceRevision), i.ResourceID, i.ResourceKind,
		fmt.Sprintf("%d", i.Generation), i.TileType, i.MatrixSet,
		fmt.Sprintf("%d", i.Zoom), fmt.Sprintf("%d", i.Column), fmt.Sprintf("%d", i.Row),
		i.StyleDigest, i.Format,
	}, "|")
}

func (i Identity) ObjectKey(prefix string) string {
	style := i.StyleDigest
	if style == "" {
		style = "default"
	}
	sum := sha256.Sum256([]byte(i.CanonicalKey()))
	leaf := hex.EncodeToString(sum[:16]) + "." + extensionForFormat(i.Format)
	parts := []string{strings.Trim(prefix, "/"), cacheLayoutVersion, i.WorkspaceID, fmt.Sprintf("w%d", i.WorkspaceRevision), i.ResourceID,
		fmt.Sprintf("%d", i.Generation), i.TileType, i.MatrixSet, fmt.Sprintf("%d", i.Zoom),
		fmt.Sprintf("%d", i.Row), fmt.Sprintf("%d", i.Column), style, leaf}
	var clean []string
	for _, part := range parts {
		if part != "" {
			clean = append(clean, sanitizePathPart(part))
		}
	}
	return strings.Join(clean, "/")
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "..", "_")
	value = strings.NewReplacer("/", "_", "\\", "_", "\x00", "_").Replace(value)
	return value
}

func extensionForFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "image/png", "png":
		return "png"
	case "image/jpeg", "image/jpg", "jpeg", "jpg":
		return "jpg"
	case "image/webp", "webp":
		return "webp"
	case "application/vnd.mapbox-vector-tile", "application/x-protobuf", "mvt", "pbf":
		return "mvt"
	default:
		return ""
	}
}

type Policy struct {
	WorkspaceQuotaBytes int64
	ResourceQuotaBytes  int64
}

type Entry struct {
	Identity       Identity
	CacheKey       string
	ObjectKey      string
	SizeBytes      int64
	ETag           string
	CreatedAt      time.Time
	LastAccessedAt time.Time
}

type CacheTier string

const (
	TierPersistent CacheTier = "persistent"
	TierRender     CacheTier = "render"
)

type Usage struct {
	SizeBytes      int64   `json:"size_bytes"`
	EntryCount     int64   `json:"entry_count"`
	QuotaBytes     int64   `json:"quota_bytes"`
	RemainingBytes int64   `json:"remaining_bytes"`
	Utilization    float64 `json:"utilization"`
}

type Stats struct {
	Enabled          bool   `json:"enabled"`
	Backend          string `json:"backend"`
	Global           Usage  `json:"global"`
	Workspace        *Usage `json:"workspace,omitempty"`
	Resource         *Usage `json:"resource,omitempty"`
	Hits             int64  `json:"hits"`
	Misses           int64  `json:"misses"`
	Writes           int64  `json:"writes"`
	WriteErrors      int64  `json:"write_errors"`
	Evictions        int64  `json:"evictions"`
	BytesEvicted     int64  `json:"bytes_evicted"`
	OversizedSkipped int64  `json:"oversized_skipped"`
	OrphansRepaired  int64  `json:"orphans_repaired"`
}

type Selector struct {
	WorkspaceID string
	ResourceID  string
	TileType    string
	MatrixSet   string
	Format      string
	StyleDigest string
	StyleName   string
	MinZoom     *int
	MaxZoom     *int
}

type DeleteResult struct {
	Entries int64 `json:"entries"`
	Bytes   int64 `json:"bytes"`
}
