package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/tilecache"
)

func newPersistentTileCache(ctx context.Context, cfg conf.PersistentCache, encryptionKey, takeoverOwnerID string) (*tilecache.Manager, error) {
	var backend tilecache.BlobStore
	var err error
	switch strings.ToLower(strings.TrimSpace(cfg.Backend)) {
	case "filesystem":
		backend, err = tilecache.NewFilesystemStore(cfg.Filesystem.Root)
	case "s3":
		backend, err = tilecache.NewS3Store(ctx, tilecache.S3Config{
			Bucket: cfg.S3.Bucket, Region: cfg.S3.Region, Endpoint: cfg.S3.Endpoint,
			UsePathStyle: cfg.S3.UsePathStyle, ServerSideEncryption: cfg.S3.ServerSideEncryption,
			KMSKeyID: cfg.S3.KMSKeyID,
		})
	default:
		return nil, fmt.Errorf("unsupported backend %q", cfg.Backend)
	}
	if err != nil {
		return nil, err
	}
	var ownership *tilecache.OwnershipConfig
	if strings.EqualFold(strings.TrimSpace(cfg.Backend), "s3") && cfg.S3.OwnershipEnabled {
		hostname, _ := os.Hostname()
		ownership = &tilecache.OwnershipConfig{
			Prefix:          cfg.S3.Prefix,
			CheckInterval:   time.Duration(cfg.S3.OwnershipCheckIntervalSec) * time.Second,
			TakeoverOwnerID: takeoverOwnerID,
			Hostname:        hostname,
		}
	}
	manager, err := tilecache.NewManager(ctx, tilecache.Config{
		DatabasePath: cfg.DatabasePath, EncryptionKey: encryptionKey,
		ObjectPrefix: cfg.S3.Prefix, MaxBytes: cfg.MaxBytes,
		AccessFlushInterval: time.Duration(cfg.AccessFlushIntervalSec) * time.Second,
		MaintenanceInterval: time.Duration(cfg.MaintenanceIntervalSec) * time.Second,
		Ownership:           ownership,
	}, backend)
	if err != nil {
		_ = backend.Close()
		return nil, err
	}
	return manager, nil
}
