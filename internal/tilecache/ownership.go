package tilecache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrOwnershipLost = errors.New("persistent tile cache ownership lost; restart required")

// OwnershipConfig controls the S3-prefix single-active-node lease. A manual
// takeover must name the exact owner observed by a previous failed startup.
type OwnershipConfig struct {
	Prefix          string
	CheckInterval   time.Duration // default 60s
	TakeoverOwnerID string
	Hostname        string
	Logger          *slog.Logger
}

const (
	ownerMarkerName          = ".neoserver-owner.json"
	legacyOwnerMarkerVersion = 1
	ownerMarkerVersion       = 3
)

type ownerMarker struct {
	SchemaVersion int       `json:"schema_version"`
	InstanceID    string    `json:"instance_id"`
	Hostname      string    `json:"hostname"`
	PID           int       `json:"pid"`
	StartedAt     time.Time `json:"started_at"`
	Released      bool      `json:"released,omitempty"`
}

type ownershipState uint8

const (
	ownershipStarting ownershipState = iota
	ownershipHeld
	ownershipLost
	ownershipReleased
)

type ownershipGuard struct {
	backend    BlobStore
	lease      LeaseStore
	key        string
	instanceID string
	startedAt  time.Time
	etag       string
	cfg        OwnershipConfig
	logger     *slog.Logger

	fence    sync.RWMutex
	state    ownershipState
	lossErr  error
	lost     chan struct{}
	lostOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newOwnershipGuard(backend BlobStore, cfg OwnershipConfig) (*ownershipGuard, error) {
	lease, ok := backend.(LeaseStore)
	if !ok {
		return nil, errors.New("tile cache ownership requires conditional object-store operations")
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	key := ownerMarkerName
	if prefix := strings.Trim(cfg.Prefix, "/"); prefix != "" {
		key = prefix + "/" + ownerMarkerName
	}
	return &ownershipGuard{
		backend: backend, lease: lease, key: key, instanceID: uuid.NewString(), startedAt: time.Now().UTC(),
		cfg: cfg, logger: logger, lost: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{}),
	}, nil
}

// acquire verifies conditional semantics and atomically claims the prefix.
func (g *ownershipGuard) acquire(ctx context.Context) error {
	if err := g.verifyConditionalSemantics(ctx); err != nil {
		return fmt.Errorf("verify S3 tile cache lease support: %w", err)
	}
	existing, readErr := g.readMarker(ctx)
	if readErr != nil && !errors.Is(readErr, ErrBlobNotFound) {
		return fmt.Errorf("read tile cache ownership marker: %w", readErr)
	}
	body, err := g.markerBody(false)
	if err != nil {
		return err
	}
	var etag string
	switch {
	case errors.Is(readErr, ErrBlobNotFound) || existing == nil:
		if g.cfg.TakeoverOwnerID != "" {
			return errors.New("tile cache ownership marker is absent; retry without --take-over-tile-cache-owner")
		}
		etag, err = g.lease.CreateLease(ctx, g.key, body)
	case existing.marker.Released:
		if g.cfg.TakeoverOwnerID != "" {
			return errors.New("tile cache ownership marker is released; retry without --take-over-tile-cache-owner")
		}
		// A released marker is reclaimed with CAS, never delete-then-create.
		// Concurrent starters cannot both replace the same generation.
		etag, err = g.lease.ReplaceLease(ctx, g.key, body, existing.etag)
	case g.cfg.TakeoverOwnerID == "":
		return fmt.Errorf(
			"S3 prefix %q is owned by %s (pid %d, owner %s, started %s); confirm that process is dead, then restart with --take-over-tile-cache-owner=%s",
			g.cfg.Prefix, existing.marker.Hostname, existing.marker.PID, existing.marker.InstanceID,
			existing.marker.StartedAt.Format(time.RFC3339), existing.marker.InstanceID)
	case g.cfg.TakeoverOwnerID != existing.marker.InstanceID:
		return fmt.Errorf("tile cache takeover owner %q does not match current owner %q", g.cfg.TakeoverOwnerID, existing.marker.InstanceID)
	default:
		etag, err = g.lease.ReplaceLease(ctx, g.key, body, existing.etag)
		if err == nil {
			g.logger.Warn("manually took over tile cache prefix ownership", "prefix", g.cfg.Prefix,
				"previousOwnerID", existing.marker.InstanceID, "previousHost", existing.marker.Hostname)
		}
	}
	if errors.Is(err, ErrLeaseConflict) {
		return errors.New("tile cache ownership changed during acquisition; retry after inspecting the current owner")
	}
	if err != nil {
		return fmt.Errorf("claim tile cache ownership marker: %w", err)
	}
	verified, err := g.readMarker(ctx)
	if err != nil || verified.marker.Released || verified.marker.InstanceID != g.instanceID || verified.etag != etag {
		return fmt.Errorf("verify tile cache ownership claim: marker does not match this process: %w", err)
	}
	g.etag = etag
	g.fence.Lock()
	g.state = ownershipHeld
	g.fence.Unlock()
	go g.verifyOwnership()
	return nil
}

type observedOwner struct {
	marker ownerMarker
	etag   string
}

func (g *ownershipGuard) readMarker(ctx context.Context) (*observedOwner, error) {
	object, err := g.lease.GetLease(ctx, g.key)
	if err != nil {
		return nil, err
	}
	var marker ownerMarker
	if err := json.Unmarshal(object.Body, &marker); err != nil {
		return nil, fmt.Errorf("ownership marker is corrupt; remove it manually only after confirming no owner is running: %w", err)
	}
	if marker.SchemaVersion < legacyOwnerMarkerVersion || marker.SchemaVersion > ownerMarkerVersion ||
		(marker.Released && marker.SchemaVersion < 3) || marker.InstanceID == "" || object.ETag == "" {
		return nil, errors.New("ownership marker is invalid; remove it manually only after confirming no owner is running")
	}
	return &observedOwner{marker: marker, etag: object.ETag}, nil
}

func (g *ownershipGuard) markerBody(released bool) ([]byte, error) {
	return json.Marshal(ownerMarker{
		SchemaVersion: ownerMarkerVersion,
		InstanceID:    g.instanceID,
		Hostname:      g.cfg.Hostname,
		PID:           os.Getpid(),
		StartedAt:     g.startedAt,
		Released:      released,
	})
}

// verifyConditionalSemantics refuses S3-compatible endpoints that accept but
// ignore the preconditions required for safe ownership.
func (g *ownershipGuard) verifyConditionalSemantics(ctx context.Context) error {
	probeKey := g.key + ".probe-" + uuid.NewString()
	defer func() { _ = g.backend.Delete(context.WithoutCancel(ctx), probeKey) }()
	first := []byte("lease-capability-a")
	second := []byte("lease-capability-b")
	etag, err := g.lease.CreateLease(ctx, probeKey, first)
	if err != nil {
		return fmt.Errorf("conditional create: %w", err)
	}
	if _, err := g.lease.CreateLease(ctx, probeKey, second); !errors.Is(err, ErrLeaseConflict) {
		return fmt.Errorf("If-None-Match was not enforced (expected conflict, got %v)", err)
	}
	if _, err := g.lease.ReplaceLease(ctx, probeKey, second, `"invalid-etag"`); !errors.Is(err, ErrLeaseConflict) {
		return fmt.Errorf("If-Match replacement was not enforced (expected conflict, got %v)", err)
	}
	newETag, err := g.lease.ReplaceLease(ctx, probeKey, second, etag)
	if err != nil {
		return fmt.Errorf("conditional replacement: %w", err)
	}
	observed, err := g.lease.GetLease(ctx, probeKey)
	if err != nil || string(observed.Body) != string(second) || observed.ETag != newETag {
		return fmt.Errorf("conditional replacement could not be verified: %w", err)
	}
	if _, err := g.lease.ReplaceLease(ctx, probeKey, first, etag); !errors.Is(err, ErrLeaseConflict) {
		return fmt.Errorf("stale If-Match replacement was not rejected (expected conflict, got %v)", err)
	}
	return nil
}

// verifyOwnership performs one read-only marker check per interval. The marker
// remains immutable until a conditional manual takeover or graceful release.
func (g *ownershipGuard) verifyOwnership() {
	defer close(g.done)
	ticker := time.NewTicker(g.cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			observed, err := g.readMarker(ctx)
			if err == nil {
				if observed.marker.Released || observed.marker.InstanceID != g.instanceID || observed.etag != g.etag {
					err = errors.New("ownership marker was replaced unexpectedly")
				}
			}
			cancel()
			if err != nil {
				g.markLost(err)
				return
			}
		case <-g.stop:
			return
		}
	}
}

// beginMutation holds a read side of the fencing lock until the durable
// mutation is complete. Once markLost returns, no admitted mutation remains.
func (g *ownershipGuard) beginMutation() (func(), error) {
	g.fence.RLock()
	if g.state != ownershipHeld {
		err := g.lossErr
		g.fence.RUnlock()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOwnershipLost, err)
		}
		return nil, ErrOwnershipLost
	}
	return g.fence.RUnlock, nil
}

func (g *ownershipGuard) markLost(cause error) {
	g.fence.Lock()
	if g.state == ownershipHeld {
		g.state = ownershipLost
		g.lossErr = cause
		g.lostOnce.Do(func() { close(g.lost) })
		g.logger.Error("tile cache ownership lost; persistent mutations are fenced until restart",
			"prefix", g.cfg.Prefix, "ownerID", g.instanceID, "error", cause)
	}
	g.fence.Unlock()
}

func (g *ownershipGuard) health() error {
	g.fence.RLock()
	defer g.fence.RUnlock()
	if g.state == ownershipHeld {
		return nil
	}
	if g.lossErr != nil {
		return fmt.Errorf("%w: %v", ErrOwnershipLost, g.lossErr)
	}
	return ErrOwnershipLost
}

func (g *ownershipGuard) lostSignal() <-chan struct{} { return g.lost }

// release fences mutations and conditionally marks this generation released.
// Never delete the shared key: some S3-compatible stores ignore conditional
// deletes, allowing a stale owner to remove a replacement owner's marker.
func (g *ownershipGuard) release() {
	g.stopOnce.Do(func() {
		close(g.stop)
		<-g.done
		g.fence.Lock()
		owned := g.state == ownershipHeld
		g.state = ownershipReleased
		g.fence.Unlock()
		if !owned {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		body, err := g.markerBody(true)
		if err == nil {
			_, err = g.lease.ReplaceLease(ctx, g.key, body, g.etag)
		}
		if err != nil && !errors.Is(err, ErrLeaseConflict) {
			g.logger.Warn("release tile cache ownership marker", "error", err)
		}
	})
}
