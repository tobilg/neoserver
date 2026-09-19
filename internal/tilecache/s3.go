package tilecache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Config struct {
	Bucket               string
	Region               string
	Endpoint             string
	UsePathStyle         bool
	ServerSideEncryption string
	KMSKeyID             string
}

// s3API is the narrow S3 client surface S3Store depends on. It exists so the
// store's key handling, encryption headers, batching, and error mapping can
// be unit-tested against a fake; *s3.Client satisfies it.
type s3API interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

type S3Store struct {
	client s3API
	bucket string
	sse    types.ServerSideEncryption
	kmsKey string
}

func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.UsePathStyle
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return &S3Store{client: client, bucket: cfg.Bucket, sse: types.ServerSideEncryption(cfg.ServerSideEncryption), kmsKey: cfg.KMSKeyID}, nil
}

func (s *S3Store) Name() string { return "s3" }

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if isS3NotFound(err) {
		return nil, ErrBlobNotFound
	}
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

func (s *S3Store) Put(ctx context.Context, key string, content []byte) error {
	input := s.putInput(key, content)
	_, err := s.client.PutObject(ctx, input)
	return err
}

func (s *S3Store) putInput(key string, content []byte) *s3.PutObjectInput {
	input := &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(content), ContentLength: aws.Int64(int64(len(content)))}
	if s.sse != "" {
		input.ServerSideEncryption = s.sse
	}
	if s.kmsKey != "" {
		input.SSEKMSKeyId = aws.String(s.kmsKey)
	}
	return input
}

func (s *S3Store) GetLease(ctx context.Context, key string) (LeaseObject, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if isS3NotFound(err) {
		return LeaseObject{}, ErrBlobNotFound
	}
	if err != nil {
		return LeaseObject{}, err
	}
	defer output.Body.Close()
	body, err := io.ReadAll(io.LimitReader(output.Body, 64<<10))
	if err != nil {
		return LeaseObject{}, err
	}
	if aws.ToString(output.ETag) == "" {
		return LeaseObject{}, errors.New("S3 ownership object has no ETag")
	}
	return LeaseObject{Body: body, ETag: aws.ToString(output.ETag)}, nil
}

func (s *S3Store) CreateLease(ctx context.Context, key string, content []byte) (string, error) {
	input := s.putInput(key, content)
	input.IfNoneMatch = aws.String("*")
	return s.putLease(ctx, input)
}

func (s *S3Store) ReplaceLease(ctx context.Context, key string, content []byte, etag string) (string, error) {
	input := s.putInput(key, content)
	input.IfMatch = aws.String(etag)
	return s.putLease(ctx, input)
}

func (s *S3Store) putLease(ctx context.Context, input *s3.PutObjectInput) (string, error) {
	output, err := s.client.PutObject(ctx, input)
	if isS3ConditionalConflict(err) {
		return "", ErrLeaseConflict
	}
	if err != nil {
		return "", err
	}
	if aws.ToString(output.ETag) == "" {
		return "", errors.New("S3 conditional write returned no ETag")
	}
	return aws.ToString(output.ETag), nil
}

func (s *S3Store) DeleteLease(ctx context.Context, key, etag string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), IfMatch: aws.String(etag)})
	if isS3ConditionalConflict(err) {
		return ErrLeaseConflict
	}
	return err
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *S3Store) DeleteBatch(ctx context.Context, keys []string) error {
	for len(keys) > 0 {
		count := min(len(keys), 1000)
		objects := make([]types.ObjectIdentifier, count)
		for i, key := range keys[:count] {
			objects[i] = types.ObjectIdentifier{Key: aws.String(key)}
		}
		output, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(s.bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if err != nil {
			return err
		}
		if len(output.Errors) > 0 {
			return fmt.Errorf("delete S3 tiles: %s", aws.ToString(output.Errors[0].Message))
		}
		keys = keys[count:]
	}
	return nil
}

func (s *S3Store) Health(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Store) Close() error { return nil }

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "404")
}

func isS3ConditionalConflict(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "PreconditionFailed", "ConditionalRequestConflict", "409", "412":
		return true
	default:
		return false
	}
}
