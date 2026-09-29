// Package objectstore talks to any S3-compatible store (SeaweedFS, Garage, AWS S3).
// Keys are always prefixed with tenant/{tenant_uuid}/ per PRD §70.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/jdolan-exalink/openvms/internal/platform/config"
)

type Store struct {
	Client *s3.Client
	Bucket string
}

func New(ctx context.Context, c config.S3Config) (*Store, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(c.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.Endpoint)
		o.UsePathStyle = c.UsePathStyle
	})
	return &Store{Client: client, Bucket: c.Bucket}, nil
}

// EnsureBucket creates the bucket when it does not exist yet.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	if err == nil {
		return nil
	}
	_, err = s.Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.Bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &owned) {
		return fmt.Errorf("create bucket %s: %w", s.Bucket, err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}

// Put stores body under key with the given content type.
func (s *Store) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// ErrNotFound is returned by Get when the key does not exist.
var ErrNotFound = errors.New("object not found")

// Get returns the object body and its content type.
func (s *Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("get %s: %w", key, err)
	}
	defer out.Body.Close()
	b, err := io.ReadAll(io.LimitReader(out.Body, 32<<20))
	if err != nil {
		return nil, "", err
	}
	return b, aws.ToString(out.ContentType), nil
}

// Delete removes the object under key. S3 reports success for missing keys.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}
