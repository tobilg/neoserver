package tilecache

import (
	"context"
	"errors"
	"io"
)

// ErrLeaseConflict reports that a conditional ownership operation lost its
// compare-and-swap race. Callers must never retry it as an unconditional
// write.
var ErrLeaseConflict = errors.New("tile cache ownership lease conflict")

type BlobStore interface {
	Name() string
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, content []byte) error
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
	Health(ctx context.Context) error
	Close() error
}

// LeaseObject is the small ownership object together with the backend version
// used for conditional replacement, including graceful release markers.
type LeaseObject struct {
	Body []byte
	ETag string
}

// LeaseStore is implemented by object stores that provide the conditional
// operations required by the S3 single-writer lease. It deliberately remains
// separate from BlobStore so filesystem caches do not pretend to provide
// distributed ownership semantics.
type LeaseStore interface {
	GetLease(ctx context.Context, key string) (LeaseObject, error)
	CreateLease(ctx context.Context, key string, content []byte) (string, error)
	ReplaceLease(ctx context.Context, key string, content []byte, etag string) (string, error)
}
