// Package storage signs S3 URLs. File contents never pass through the gateway.
package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/ulvs/fling/gateway/internal/config"
)

var ErrNotFound = errors.New("object not found")

// Objects is what the API needs from object storage.
type Objects interface {
	// PresignPut returns a URL and the headers the client must send with its PUT.
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, http.Header, error)
	// PresignGet returns a URL that downloads the object as an attachment named downloadName.
	PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error)
	// Size returns the stored object's size, or ErrNotFound.
	Size(ctx context.Context, key string) (int64, error)
	// Ping checks that the bucket is reachable with the configured credentials.
	Ping(ctx context.Context) error
}

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewS3(ctx context.Context, cfg config.S3) (*S3, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
		// Optional checksums (aws-chunked trailers) are rejected by MinIO builds, Backblaze B2 and Cloudflare R2.
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = cfg.PathStyle
	})
	return &S3{client: client, presign: s3.NewPresignClient(client), bucket: cfg.Bucket}, nil
}

func (s *S3) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, http.Header, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", nil, err
	}
	headers := http.Header{}
	for name, values := range req.SignedHeader {
		// Host is set by the HTTP client from the URL.
		if !strings.EqualFold(name, "Host") {
			headers[name] = values
		}
	}
	return req.URL, headers, nil
}

func (s *S3) PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(s.bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(ContentDisposition(downloadName)),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3) Size(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var notFound *types.NotFound
		var resp *smithyhttp.ResponseError
		if errors.As(err, &notFound) || (errors.As(err, &resp) && resp.HTTPStatusCode() == http.StatusNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

func (s *S3) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

// ContentDisposition builds an attachment header with an ASCII fallback and the exact UTF-8 name.
func ContentDisposition(name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || r < 0x20 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, attrEscape(name))
}

// attrEscape percent-encodes everything outside RFC 5987's attr-char set.
func attrEscape(s string) string {
	const attrChars = "!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x80 && (unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || strings.IndexByte(attrChars, c) >= 0) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
