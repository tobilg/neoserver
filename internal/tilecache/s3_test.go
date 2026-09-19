package tilecache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type fakeS3 struct {
	objects    map[string][]byte
	etags      map[string]string
	version    int
	lastPut    *s3.PutObjectInput
	deleteReqs [][]types.ObjectIdentifier
	headErr    error
	deleteErr  error
	batchErrs  []types.Error
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}, etags: map[string]string{}} }

type s3NotFoundError struct{ code string }

func (e *s3NotFoundError) Error() string                 { return e.code }
func (e *s3NotFoundError) ErrorCode() string             { return e.code }
func (e *s3NotFoundError) ErrorMessage() string          { return e.code }
func (e *s3NotFoundError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func (f *fakeS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	body, ok := f.objects[aws.ToString(params.Key)]
	if !ok {
		return nil, &s3NotFoundError{code: "NoSuchKey"}
	}
	key := aws.ToString(params.Key)
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(body))), ETag: aws.String(f.etags[key])}, nil
}

func (f *fakeS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.lastPut = params
	key := aws.ToString(params.Key)
	if aws.ToString(params.IfNoneMatch) == "*" {
		if _, exists := f.objects[key]; exists {
			return nil, &s3NotFoundError{code: "PreconditionFailed"}
		}
	}
	if expected := aws.ToString(params.IfMatch); expected != "" && f.etags[key] != expected {
		return nil, &s3NotFoundError{code: "PreconditionFailed"}
	}
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	f.version++
	etag := fmt.Sprintf("\"etag-%d\"", f.version)
	f.objects[key] = body
	f.etags[key] = etag
	return &s3.PutObjectOutput{ETag: aws.String(etag)}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	key := aws.ToString(params.Key)
	if expected := aws.ToString(params.IfMatch); expected != "" && f.etags[key] != expected {
		return nil, &s3NotFoundError{code: "PreconditionFailed"}
	}
	delete(f.objects, key)
	delete(f.etags, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, params *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	f.deleteReqs = append(f.deleteReqs, params.Delete.Objects)
	if len(f.batchErrs) > 0 {
		return &s3.DeleteObjectsOutput{Errors: f.batchErrs}, nil
	}
	for _, obj := range params.Delete.Objects {
		key := aws.ToString(obj.Key)
		delete(f.objects, key)
		delete(f.etags, key)
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) HeadBucket(_ context.Context, _ *s3.HeadBucketInput, _ ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	if f.headErr != nil {
		return nil, f.headErr
	}
	return &s3.HeadBucketOutput{}, nil
}

func newFakeS3Store(fake *fakeS3, sse, kmsKey string) *S3Store {
	return &S3Store{client: fake, bucket: "test-bucket", sse: types.ServerSideEncryption(sse), kmsKey: kmsKey}
}

func TestS3StoreGetPutRoundTrip(t *testing.T) {
	fake := newFakeS3()
	store := newFakeS3Store(fake, "", "")
	ctx := context.Background()

	if err := store.Put(ctx, "prefix/tile.png", []byte("payload")); err != nil {
		t.Fatalf("put: %v", err)
	}
	reader, err := store.Get(ctx, "prefix/tile.png")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	if string(body) != "payload" {
		t.Fatalf("body = %q", body)
	}
	if fake.lastPut.ServerSideEncryption != "" || fake.lastPut.SSEKMSKeyId != nil {
		t.Fatal("encryption headers must be absent when not configured")
	}
}

func TestS3StoreEncryptionHeaders(t *testing.T) {
	fake := newFakeS3()
	store := newFakeS3Store(fake, "aws:kms", "key-1")
	if err := store.Put(context.Background(), "k", []byte("x")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if fake.lastPut.ServerSideEncryption != types.ServerSideEncryptionAwsKms {
		t.Errorf("SSE = %q, want aws:kms", fake.lastPut.ServerSideEncryption)
	}
	if aws.ToString(fake.lastPut.SSEKMSKeyId) != "key-1" {
		t.Errorf("KMS key = %q, want key-1", aws.ToString(fake.lastPut.SSEKMSKeyId))
	}
}

func TestS3StoreGetNotFoundMapsToErrBlobNotFound(t *testing.T) {
	store := newFakeS3Store(newFakeS3(), "", "")
	if _, err := store.Get(context.Background(), "missing"); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("err = %v, want ErrBlobNotFound", err)
	}
}

func TestS3StoreDeleteBatchChunksAt1000(t *testing.T) {
	fake := newFakeS3()
	store := newFakeS3Store(fake, "", "")
	keys := make([]string, 1500)
	for i := range keys {
		keys[i] = fmt.Sprintf("k-%d", i)
	}
	if err := store.DeleteBatch(context.Background(), keys); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if len(fake.deleteReqs) != 2 || len(fake.deleteReqs[0]) != 1000 || len(fake.deleteReqs[1]) != 500 {
		t.Fatalf("chunking wrong: %d requests", len(fake.deleteReqs))
	}
}

func TestS3StoreDeleteBatchSurfacesPerObjectErrors(t *testing.T) {
	fake := newFakeS3()
	fake.batchErrs = []types.Error{{Message: aws.String("access denied")}}
	store := newFakeS3Store(fake, "", "")
	if err := store.DeleteBatch(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("err = %v, want per-object error surfaced", err)
	}
}

func TestS3StoreHealth(t *testing.T) {
	fake := newFakeS3()
	store := newFakeS3Store(fake, "", "")
	if err := store.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	fake.headErr = errors.New("no bucket")
	if err := store.Health(context.Background()); err == nil {
		t.Fatal("expected health error")
	}
}

func TestS3StoreConditionalLeaseOperations(t *testing.T) {
	store := newFakeS3Store(newFakeS3(), "", "")
	ctx := context.Background()
	etag, err := store.CreateLease(ctx, "owner", []byte("one"))
	if err != nil || etag == "" {
		t.Fatalf("create lease = %q, %v", etag, err)
	}
	if _, err := store.CreateLease(ctx, "owner", []byte("two")); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("duplicate create = %v", err)
	}
	if _, err := store.ReplaceLease(ctx, "owner", []byte("two"), `"wrong"`); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("wrong replace = %v", err)
	}
	newETag, err := store.ReplaceLease(ctx, "owner", []byte("two"), etag)
	if err != nil || newETag == etag {
		t.Fatalf("replace lease = %q, %v", newETag, err)
	}
	object, err := store.GetLease(ctx, "owner")
	if err != nil || string(object.Body) != "two" || object.ETag != newETag {
		t.Fatalf("get lease = %+v, %v", object, err)
	}
	if err := store.DeleteLease(ctx, "owner", etag); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("wrong delete = %v", err)
	}
	if err := store.DeleteLease(ctx, "owner", newETag); err != nil {
		t.Fatalf("delete lease = %v", err)
	}
}

func TestIsS3NotFound(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("plain"), false},
		{&s3NotFoundError{code: "NoSuchKey"}, true},
		{&s3NotFoundError{code: "NotFound"}, true},
		{&s3NotFoundError{code: "404"}, true},
		{&s3NotFoundError{code: "AccessDenied"}, false},
		{fmt.Errorf("wrapped: %w", &s3NotFoundError{code: "NoSuchKey"}), true},
	}
	for _, tt := range tests {
		if got := isS3NotFound(tt.err); got != tt.want {
			t.Errorf("isS3NotFound(%v) = %t, want %t", tt.err, got, tt.want)
		}
	}
}

func TestIsS3ConditionalConflict(t *testing.T) {
	for _, code := range []string{"PreconditionFailed", "ConditionalRequestConflict", "409", "412"} {
		if !isS3ConditionalConflict(&s3NotFoundError{code: code}) {
			t.Errorf("code %s was not recognized", code)
		}
	}
	if isS3ConditionalConflict(&s3NotFoundError{code: "AccessDenied"}) {
		t.Fatal("AccessDenied is not a conditional conflict")
	}
}
