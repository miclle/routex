// Command storage-acceptance-fixture prepares a fresh, versioned local S3 bucket.
// It is a test controller, not part of the RouteX executable or storage contract.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var bucketName = regexp.MustCompile(`^routex-acceptance-[a-f0-9]{24}$`)

type receipt struct {
	State             string    `json:"state"`
	Bucket            string    `json:"bucket"`
	Versioning        string    `json:"versioning"`
	AnonymousDenied   bool      `json:"anonymous_denied"`
	WrongSecretDenied bool      `json:"wrong_secret_denied"`
	CreatedAt         time.Time `json:"created_at"`
}

func main() {
	endpoint := flag.String("endpoint", "", "owned HTTP loopback S3 endpoint")
	bucket := flag.String("bucket", "", "fresh routex-acceptance-<24 lowercase hex> bucket")
	output := flag.String("receipt", "", "new private receipt path")
	mode := flag.String("mode", "prepare", "prepare, snapshot, later or cleanup")
	input := flag.String("input", "", "private exact-object input receipt")
	flag.Parse()
	var err error
	if *mode == "prepare" {
		err = run(*endpoint, *bucket, *output)
	} else {
		err = runFacts(*mode, *endpoint, *bucket, *input, *output)
	}
	if err != nil {
		// SDK errors can include request data. Keep the external result status-only.
		fmt.Fprintln(os.Stderr, "Local versioned-storage fixture preparation failed")
		os.Exit(1)
	}
}

func run(endpoint, bucket, output string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return errors.New("invalid local endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" || !bucketName.MatchString(bucket) || output == "" {
		return errors.New("invalid local fixture identity")
	}
	access, secret := os.Getenv("ROUTEX_ACCEPTANCE_ACCESS_KEY"), os.Getenv("ROUTEX_ACCEPTANCE_SECRET_KEY")
	if len(access) < 16 || len(secret) < 32 {
		return errors.New("missing disposable credentials")
	}
	if err = validatePrivateReceipt(output); err != nil {
		return err
	}

	// Create the receipt exclusively before any side effect; ambiguous failures
	// leave it incomplete rather than treating an existing bucket as ours.
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err = json.NewEncoder(file).Encode(receipt{State: "pending", Bucket: bucket, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	clientFor := func(key string) *s3.Client {
		return s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true,
			Credentials: credentials.NewStaticCredentialsProvider(access, key, ""), HTTPClient: httpClient,
			RetryMaxAttempts: 1, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	}
	client := clientFor(secret)
	// Reject existing or unreadable buckets before creation. The controller owns
	// this single-use server and supplies a fresh random bucket identity.
	_, headErr := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	var headStatus interface{ HTTPStatusCode() int }
	if !errors.As(headErr, &headStatus) || headStatus.HTTPStatusCode() != http.StatusNotFound {
		return errors.New("bucket was not confirmed absent")
	}
	if _, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		return err
	}
	if _, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		return err
	}
	versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil || versioning.Status != types.BucketVersioningStatusEnabled {
		return errors.New("versioning not confirmed")
	}
	// Authenticate negatively against the actual server, not a look-alike fixture.
	_, wrongErr := clientFor(secret+"-wrong").ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), MaxKeys: aws.Int32(1)})
	var wrongStatus interface{ HTTPStatusCode() int }
	if !errors.As(wrongErr, &wrongStatus) || wrongStatus.HTTPStatusCode() != http.StatusForbidden {
		return errors.New("wrong secret was not denied")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/"+bucket+"?versions", nil)
	if err != nil {
		return err
	}
	anonymous, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusForbidden {
		return errors.New("anonymous access was not denied")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return err
	}
	if err = file.Truncate(0); err != nil {
		return err
	}
	if err = json.NewEncoder(file).Encode(receipt{State: "prepared", Bucket: bucket, Versioning: string(versioning.Status), AnonymousDenied: true, WrongSecretDenied: true, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return file.Sync()
}

func validatePrivateReceipt(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("receipt path must be absolute and canonical")
	}
	parent := filepath.Dir(output)
	info, err := os.Lstat(parent)
	physical, physicalErr := filepath.EvalSymlinks(parent)
	if err != nil || physicalErr != nil || !info.IsDir() || info.Mode().Perm() != 0700 || physical != parent {
		return errors.New("receipt parent must be private and physical")
	}
	return nil
}
