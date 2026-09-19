package tilecache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
)

// fakeBlobStore is an in-memory BlobStore with per-operation error injection.
// It backs manager rollback tests and the S3 ownership-guard tests.
type fakeBlobStore struct {
	mu        sync.Mutex
	objects   map[string][]byte
	etags     map[string]string
	version   int64
	putErr    error
	getErr    error
	delErr    error
	healthEr  error
	putCalls  []string
	delCalls  []string
	leaseGets []string
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{objects: map[string][]byte{}, etags: map[string]string{}}
}

func (f *fakeBlobStore) Name() string { return "fake" }

func (f *fakeBlobStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	body, ok := f.objects[key]
	if !ok {
		return nil, ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), body...))), nil
}

func (f *fakeBlobStore) Put(_ context.Context, key string, content []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls = append(f.putCalls, key)
	if f.putErr != nil {
		return f.putErr
	}
	f.objects[key] = append([]byte(nil), content...)
	f.etags[key] = f.nextETag()
	return nil
}

func (f *fakeBlobStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delCalls = append(f.delCalls, key)
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.objects, key)
	delete(f.etags, key)
	return nil
}

func (f *fakeBlobStore) nextETag() string {
	f.version++
	return fmt.Sprintf("\"fake-%d\"", f.version)
}

func (f *fakeBlobStore) GetLease(_ context.Context, key string) (LeaseObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseGets = append(f.leaseGets, key)
	if f.getErr != nil {
		return LeaseObject{}, f.getErr
	}
	body, ok := f.objects[key]
	if !ok {
		return LeaseObject{}, ErrBlobNotFound
	}
	return LeaseObject{Body: append([]byte(nil), body...), ETag: f.etags[key]}, nil
}

func (f *fakeBlobStore) CreateLease(_ context.Context, key string, content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls = append(f.putCalls, key)
	if f.putErr != nil {
		return "", f.putErr
	}
	if _, exists := f.objects[key]; exists {
		return "", ErrLeaseConflict
	}
	etag := f.nextETag()
	f.objects[key] = append([]byte(nil), content...)
	f.etags[key] = etag
	return etag, nil
}

func (f *fakeBlobStore) ReplaceLease(_ context.Context, key string, content []byte, etag string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls = append(f.putCalls, key)
	if f.putErr != nil {
		return "", f.putErr
	}
	if current, exists := f.etags[key]; !exists || current != etag {
		return "", ErrLeaseConflict
	}
	newETag := f.nextETag()
	f.objects[key] = append([]byte(nil), content...)
	f.etags[key] = newETag
	return newETag, nil
}

func (f *fakeBlobStore) DeleteLease(_ context.Context, key, etag string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delCalls = append(f.delCalls, key)
	if f.delErr != nil {
		return f.delErr
	}
	if current, exists := f.etags[key]; !exists || current != etag {
		return ErrLeaseConflict
	}
	delete(f.objects, key)
	delete(f.etags, key)
	return nil
}

func (f *fakeBlobStore) DeleteBatch(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := f.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeBlobStore) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthEr
}

func (f *fakeBlobStore) Close() error { return nil }

func (f *fakeBlobStore) setPutErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putErr = err
}

func (f *fakeBlobStore) setGetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getErr = err
}

func (f *fakeBlobStore) ownershipCallCounts() (puts, gets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.putCalls), len(f.leaseGets)
}

func (f *fakeBlobStore) object(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	return body, ok
}
