package tilecache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

func minioStore(t *testing.T) *S3Store {
	t.Helper()
	endpoint := os.Getenv("NEOSRV_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("NEOSRV_MINIO_ENDPOINT is not set")
	}
	region := "us-east-1"
	bucket := "neoserver-lease-test"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		t.Fatal(err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	var createErr error
	for ctx.Err() == nil {
		_, createErr = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		if createErr == nil || strings.Contains(createErr.Error(), "BucketAlreadyOwnedByYou") || strings.Contains(createErr.Error(), "BucketAlreadyExists") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if createErr != nil && !strings.Contains(createErr.Error(), "BucketAlready") {
		t.Fatalf("create MinIO bucket: %v", createErr)
	}
	store, err := NewS3Store(context.Background(), S3Config{Bucket: bucket, Region: region, Endpoint: endpoint, UsePathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestMinIOConditionalLeaseSemantics(t *testing.T) {
	store := minioStore(t)
	key := "integration/conditional-" + uuid.NewString()
	ctx := context.Background()
	etag, err := store.CreateLease(ctx, key, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Delete(ctx, key) }()
	if _, err := store.CreateLease(ctx, key, []byte("second")); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("If-None-Match conflict = %v", err)
	}
	if _, err := store.ReplaceLease(ctx, key, []byte("second"), `"wrong"`); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("wrong If-Match replacement = %v", err)
	}
	newETag, err := store.ReplaceLease(ctx, key, []byte("second"), etag)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceLease(ctx, key, []byte("released"), etag); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale If-Match release = %v", err)
	}
	if _, err := store.ReplaceLease(ctx, key, []byte("released"), newETag); err != nil {
		t.Fatal(err)
	}
}

func TestMinIOOwnershipRaceAndExactManualTakeover(t *testing.T) {
	store := minioStore(t)
	prefix := "integration/race-" + uuid.NewString()
	cfg := OwnershipConfig{Prefix: prefix, CheckInterval: time.Hour, Hostname: "node-a", Logger: discardLogger()}
	guards := []*ownershipGuard{mustGuard(t, store, cfg), mustGuard(t, store, cfg)}
	type result struct {
		guard *ownershipGuard
		err   error
	}
	results := make(chan result, len(guards))
	var wg sync.WaitGroup
	for _, guard := range guards {
		wg.Add(1)
		go func(guard *ownershipGuard) {
			defer wg.Done()
			results <- result{guard: guard, err: guard.acquire(context.Background())}
		}(guard)
	}
	wg.Wait()
	close(results)
	var winner *ownershipGuard
	for item := range results {
		if item.err == nil {
			if winner != nil {
				t.Fatal("two processes acquired the same prefix")
			}
			winner = item.guard
		}
	}
	if winner == nil {
		t.Fatal("no process acquired the empty prefix")
	}

	takeoverCfg := cfg
	takeoverCfg.Hostname = "node-b"
	takeoverCfg.TakeoverOwnerID = winner.instanceID
	replacement := mustGuard(t, store, takeoverCfg)
	if err := replacement.acquire(context.Background()); err != nil {
		t.Fatalf("exact manual takeover: %v", err)
	}
	winner.release()
	observed, err := store.GetLease(context.Background(), replacement.key)
	if err != nil || !strings.Contains(string(observed.Body), replacement.instanceID) {
		t.Fatalf("old owner removed replacement marker: %s, %v", observed.Body, err)
	}
	replacement.release()
	marker, err := replacement.readMarker(context.Background())
	if err != nil || !marker.marker.Released {
		t.Fatalf("replacement release did not leave a released generation: %v", err)
	}
	// Multiple processes racing a released generation still have one winner.
	guards = []*ownershipGuard{mustGuard(t, store, cfg), mustGuard(t, store, cfg)}
	results = make(chan result, len(guards))
	for _, guard := range guards {
		wg.Add(1)
		go func(guard *ownershipGuard) {
			defer wg.Done()
			results <- result{guard, guard.acquire(context.Background())}
		}(guard)
	}
	wg.Wait()
	close(results)
	winners := 0
	for item := range results {
		if item.err == nil {
			winners++
			defer item.guard.release()
		}
	}
	if winners != 1 {
		t.Fatalf("released generation race had %d winners", winners)
	}
}

func TestMinIOOwnershipCapabilityProbe(t *testing.T) {
	store := minioStore(t)
	guard := mustGuard(t, store, OwnershipConfig{
		Prefix: "integration/capability-" + uuid.NewString(), CheckInterval: time.Hour,
		Hostname: "probe", Logger: discardLogger(),
	})
	if err := guard.verifyConditionalSemantics(context.Background()); err != nil {
		t.Fatalf("MinIO conditional capability probe: %v", err)
	}
	if guard.instanceID == "" {
		t.Fatal(fmt.Errorf("guard owner ID was not initialized"))
	}
}
