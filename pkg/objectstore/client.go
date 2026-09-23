// Package objectstore performs bounded, guarded S3 operations on generated keys.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/miclle/routex/pkg/upstream"
)

const MaxBytes = 2 << 20

var (
	ErrConfig      = errors.New("invalid storage configuration")
	ErrUnavailable = errors.New("object storage unavailable")
	ErrNotFound    = errors.New("object not found")
	ErrConflict    = errors.New("object operation conflict")
)

type Config struct{ Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string }

func (Config) String() string { return "object storage configuration (redacted)" }

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var regionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func Validate(c Config, allowPrivate bool) error {
	endpoint, err := upstream.ValidateBaseURL(c.Endpoint, allowPrivate)
	if err != nil || (endpoint.Path != "" && endpoint.Path != "/") || !regionPattern.MatchString(c.Region) || !bucketPattern.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || len(c.Prefix) > 512 || strings.HasPrefix(c.Prefix, "/") || strings.ContainsAny(c.Prefix, "\\\r\n\x00") || len(c.AccessKey) > 256 || len(c.SecretKey) > 4096 || c.AccessKey == "" || c.SecretKey == "" || strings.ContainsAny(c.AccessKey+c.SecretKey, "\r\n\x00") {
		return ErrConfig
	}
	for _, part := range strings.Split(c.Prefix, "/") {
		if part == "." || part == ".." {
			return ErrConfig
		}
	}
	return nil
}

type Client struct {
	client         *s3.Client
	http           *http.Client
	bucket, prefix string
}

func New(c Config, allowPrivate bool) (*Client, error) {
	if err := Validate(c, allowPrivate); err != nil {
		return nil, err
	}
	transport := upstream.NewClient(allowPrivate)
	sdk := s3.New(s3.Options{Region: c.Region, BaseEndpoint: aws.String(strings.TrimRight(c.Endpoint, "/")), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""), HTTPClient: transport, RetryMaxAttempts: 1, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	return &Client{client: sdk, http: transport, bucket: c.Bucket, prefix: c.Prefix}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) key(id string) (string, error) {
	if len(id) < 4 || len(id) > 80 {
		return "", ErrConfig
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "", ErrConfig
		}
	}
	prefix := strings.TrimSuffix(c.prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return prefix + "routex/" + id, nil
}

type Object struct {
	Data               []byte
	VersionID, OwnerID string
	Size               int64
}

func (c *Client) Put(ctx context.Context, id, mime string, data []byte) (string, error) {
	key, err := c.key(id)
	if err != nil || len(data) > MaxBytes {
		return "", ErrConfig
	}
	result, err := c.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String(mime), IfNoneMatch: aws.String("*"), Metadata: map[string]string{"routex-id": id}})
	if err != nil {
		return "", safeError(err)
	}
	version := aws.ToString(result.VersionId)
	if !validVersion(version) {
		return "", ErrUnavailable
	}
	return version, nil
}
func (c *Client) Get(ctx context.Context, id, version string) (Object, error) {
	key, err := c.key(id)
	if err != nil || !validVersion(version) {
		return Object{}, ErrConfig
	}
	input := &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if version != "" {
		input.VersionId = aws.String(version)
	}
	result, err := c.client.GetObject(ctx, input)
	if err != nil {
		return Object{}, safeError(err)
	}
	defer func() { _ = result.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(result.Body, MaxBytes+1))
	if err != nil || len(data) > MaxBytes || !validVersion(aws.ToString(result.VersionId)) {
		return Object{}, ErrUnavailable
	}
	return Object{Data: data, VersionID: aws.ToString(result.VersionId), OwnerID: result.Metadata["routex-id"], Size: int64(len(data))}, nil
}
func (c *Client) Head(ctx context.Context, id, version string) (Object, error) {
	key, err := c.key(id)
	if err != nil || !validVersion(version) {
		return Object{}, ErrConfig
	}
	input := &s3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if version != "" {
		input.VersionId = aws.String(version)
	}
	result, err := c.client.HeadObject(ctx, input)
	if err != nil {
		return Object{}, safeError(err)
	}
	size := aws.ToInt64(result.ContentLength)
	if size < 0 || !validVersion(aws.ToString(result.VersionId)) {
		return Object{}, ErrUnavailable
	}
	return Object{VersionID: aws.ToString(result.VersionId), OwnerID: result.Metadata["routex-id"], Size: size}, nil
}

// Delete only deletes the recorded owned object/version. An ambiguous upload
// first resolves its version and checks ownership instead of deleting blindly.
func (c *Client) Delete(ctx context.Context, id, version string) error {
	object, err := c.Head(ctx, id, version)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if object.OwnerID != id {
		return ErrConflict
	}
	key, _ := c.key(id)
	input := &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if object.VersionID != "" {
		input.VersionId = aws.String(object.VersionID)
	}
	_, err = c.client.DeleteObject(ctx, input)
	if err != nil {
		return safeError(err)
	}
	return nil
}
func validVersion(value string) bool {
	return len(value) <= 1024 && !strings.ContainsAny(value, "\r\n\x00")
}
func safeError(err error) error {
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchVersion":
			return ErrNotFound
		case "PreconditionFailed", "ConditionalRequestConflict":
			return ErrConflict
		}
	}
	return ErrUnavailable
}
