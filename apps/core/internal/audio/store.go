// Package audio wires vvaves's voice and object store to
// packages/go/audioreader, the shared caching, priming and streaming layer.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/lalternative/packages/go/audioreader"
)

// Store keeps readings in an S3-compatible bucket. Only Upload and Download
// are here because that is the whole of audioreader.Store: presigning,
// deleting and listing are no business of serving audio, and retention is a
// bucket lifecycle rule rather than something this has to know about.
type Store struct {
	client *s3.Client
	bucket string
	prefix string
}

var _ audioreader.Store = (*Store)(nil)

// NewStoreFromEnv builds a Store from S3_ENDPOINT, S3_REGION, S3_ACCESS_KEY,
// S3_SECRET_KEY and S3_BUCKET. Every key is put under prefix, so that two
// voices never share a reading: the cache key names the text, not who reads
// it, and an opening kept from one voice would play before the rest in the
// other.
//
// It returns (nil, nil) when none of them are set: a vvaves with no bucket
// still reads text aloud, it just pays for every reading. Half a
// configuration is an error rather than a silent fallback — it means someone
// meant to have a cache and will not get one.
//
// The concrete *Store is returned rather than audioreader.Store so a nil
// result stays nil when the caller tests it. Boxing a nil pointer in an
// interface yields a non-nil interface, and every `== nil` guard downstream
// would pass before dereferencing it.
func NewStoreFromEnv(prefix string) (*Store, error) {
	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("S3_REGION")

	if endpoint == "" && accessKey == "" && secretKey == "" && bucket == "" {
		return nil, nil
	}
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, errors.New("S3 configuration incomplete: S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY and S3_BUCKET are all required")
	}
	if region == "" {
		region = "fr-par"
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		// The SDK default (WhenSupported) adds a CRC32 to every PutObject and
		// delivers it as an aws-chunked trailer, which wraps the body in chunk
		// framing under Transfer-Encoding: chunked. S3-compatible stores that
		// do not decode that framing reject the PUT outright, at any size.
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}

	return &Store{
		client: s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}),
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (s *Store) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.prefix + key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// Download reports a missing key as audioreader.ErrNotFound, which is what
// the reader checks for to tell an empty cache apart from a broken one: the
// first is the normal first listen, the second is worth a log line.
func (s *Store) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.prefix + key),
	})
	if err != nil {
		var noSuchKey *s3types.NoSuchKey
		var notFound *s3types.NotFound
		if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
			return nil, audioreader.ErrNotFound
		}
		return nil, fmt.Errorf("download %s: %w", key, err)
	}
	return out.Body, nil
}
