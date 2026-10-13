package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type storageFact struct {
	entity.StorageObject
	RecordedVersionID string `json:"version_id"`
}
type factsInput struct {
	Objects    []storageFact  `json:"objects"`
	RequestIDs []string       `json:"request_ids"`
	Later      []laterVersion `json:"later"`
}
type factsReceipt struct {
	Objects                []storageFact        `json:"objects"`
	Calls                  []entity.CallRecord  `json:"calls"`
	Attempts               []entity.CallAttempt `json:"attempts"`
	Later                  []laterVersion       `json:"later,omitempty"`
	ExactVersionsAbsent    bool                 `json:"exact_versions_absent,omitempty"`
	LaterVersionsPreserved bool                 `json:"later_versions_preserved,omitempty"`
	DeleteMarkersAbsent    bool                 `json:"delete_markers_absent,omitempty"`
}
type laterVersion struct {
	ObjectID  string `json:"object_id"`
	VersionID string `json:"version_id"`
	SHA256    string `json:"sha256"`
}

var factID = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,64}$`)

func readFactsInput(path string) (factsInput, error) {
	var input factsInput
	f, err := os.Open(path)
	if err != nil {
		return input, err
	}
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	if err = d.Decode(&input); err != nil {
		return input, err
	}
	if d.Decode(new(any)) != io.EOF || len(input.Objects) > 4 || len(input.RequestIDs) > 8 || len(input.Later) > 4 {
		return input, errors.New("invalid input bound")
	}
	seen := map[string]bool{}
	for _, row := range input.Objects {
		if !factID.MatchString(row.ID) || !factID.MatchString(row.OwnerID) || seen[row.ID] {
			return input, errors.New("invalid object identity")
		}
		seen[row.ID] = true
	}
	for _, id := range input.RequestIDs {
		if !factID.MatchString(id) {
			return input, errors.New("invalid request identity")
		}
	}
	return input, nil
}

func runFacts(mode, endpoint, bucket, inputPath, output string) error {
	if mode != "snapshot" && mode != "later" && mode != "cleanup" {
		return errors.New("unknown fixture mode")
	}
	if err := validatePrivateReceipt(output); err != nil {
		return err
	}
	input, err := readFactsInput(inputPath)
	if err != nil {
		return err
	}
	if len(input.Objects) == 0 {
		return errors.New("objects required")
	}
	dsn := os.Getenv("ROUTEX_ACCEPTANCE_DSN")
	if err = validateFixtureDSN(dsn); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := database.Open(ctx, "postgres", dsn)
	if err != nil {
		return err
	}
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	result := factsReceipt{Objects: []storageFact{}, Calls: []entity.CallRecord{}, Attempts: []entity.CallAttempt{}}
	for _, expected := range input.Objects {
		var row entity.StorageObject
		if err = db.WithContext(ctx).First(&row, "id = ? AND owner_kind = ? AND owner_id = ?", expected.ID, expected.OwnerKind, expected.OwnerID).Error; err != nil {
			return err
		}
		if expected.RecordedVersionID != "" && (row.VersionID != expected.RecordedVersionID || row.RevisionID != expected.RevisionID || row.SHA256 != expected.SHA256) {
			return errors.New("recorded storage identity changed")
		}
		result.Objects = append(result.Objects, storageFact{StorageObject: row, RecordedVersionID: row.VersionID})
	}
	if len(input.RequestIDs) > 0 {
		if err = db.WithContext(ctx).Where("request_id IN ?", input.RequestIDs).Order("request_id").Find(&result.Calls).Error; err != nil {
			return err
		}
		if err = db.WithContext(ctx).Where("request_id IN ?", input.RequestIDs).Order("request_id, attempt_number, id").Find(&result.Attempts).Error; err != nil {
			return err
		}
		if len(result.Calls) != len(input.RequestIDs) || len(result.Attempts) != len(input.RequestIDs) {
			return errors.New("missing exact single-attempt facts")
		}
	}
	if mode == "snapshot" {
		return writePrivateReceipt(output, result)
	}
	server, err := url.Parse(endpoint)
	if err != nil || server.Scheme != "http" || server.User != nil || server.RawQuery != "" || server.Fragment != "" || server.Path != "" || net.ParseIP(server.Hostname()) == nil || !net.ParseIP(server.Hostname()).IsLoopback() || !bucketName.MatchString(bucket) {
		return errors.New("invalid fixture endpoint")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(os.Getenv("ROUTEX_ACCEPTANCE_ACCESS_KEY"), os.Getenv("ROUTEX_ACCEPTANCE_SECRET_KEY"), ""), HTTPClient: &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, RetryMaxAttempts: 1, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	for _, row := range result.Objects {
		var revision entity.StorageRevision
		if err = db.WithContext(ctx).First(&revision, "id = ?", row.RevisionID).Error; err != nil {
			return err
		}
		if revision.Endpoint != endpoint || revision.Bucket != bucket {
			return errors.New("foreign storage revision")
		}
		key := strings.TrimSuffix(revision.Prefix, "/") + "/routex/" + row.ID
		if mode == "later" {
			if row.State != "ready" || !row.UploadConfirmed || row.VersionID == "" {
				return errors.New("object not confirmed ready")
			}
			original, getErr := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(row.VersionID)})
			if getErr != nil {
				return getErr
			}
			data, readErr := io.ReadAll(io.LimitReader(original.Body, 2<<20+1))
			_ = original.Body.Close()
			if readErr != nil || len(data) > 2<<20 || int64(len(data)) != row.Size || digest(data) != row.SHA256 || aws.ToString(original.VersionId) != row.VersionID || original.Metadata["routex-id"] != row.ID {
				return errors.New("original version mismatch")
			}
			// Distinct controller bytes make latest-version substitution detectable.
			laterData := append(append([]byte{}, data...), []byte("\ncontrolled-later-version")...)
			put, putErr := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(laterData), ContentType: aws.String(row.MIME), Metadata: map[string]string{"routex-id": row.ID}})
			if putErr != nil || aws.ToString(put.VersionId) == "" || aws.ToString(put.VersionId) == row.VersionID {
				return errors.New("later version missing")
			}
			result.Later = append(result.Later, laterVersion{ObjectID: row.ID, VersionID: aws.ToString(put.VersionId), SHA256: digest(laterData)})
		} else if mode == "cleanup" {
			if row.State != "deleted" {
				return errors.New("cleanup not acknowledged")
			}
			_, headErr := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(row.VersionID)})
			var status interface{ HTTPStatusCode() int }
			if !errors.As(headErr, &status) || status.HTTPStatusCode() != http.StatusNotFound {
				return errors.New("original version not absent")
			}
			var later *laterVersion
			for i := range input.Later {
				if input.Later[i].ObjectID == row.ID {
					later = &input.Later[i]
				}
			}
			if later == nil || later.VersionID == "" || later.VersionID == row.VersionID {
				return errors.New("later receipt missing")
			}
			got, getErr := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), VersionId: aws.String(later.VersionID)})
			if getErr != nil {
				return getErr
			}
			data, readErr := io.ReadAll(io.LimitReader(got.Body, 2<<20+128))
			_ = got.Body.Close()
			if readErr != nil || digest(data) != later.SHA256 || aws.ToString(got.VersionId) != later.VersionID {
				return errors.New("later version changed")
			}
			versions, listErr := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(key), MaxKeys: aws.Int32(10)})
			if listErr != nil || aws.ToBool(versions.IsTruncated) || len(versions.DeleteMarkers) != 0 {
				return errors.New("delete marker or version overflow")
			}
			if len(versions.Versions) != 1 || aws.ToString(versions.Versions[0].Key) != key || aws.ToString(versions.Versions[0].VersionId) != later.VersionID {
				return errors.New("unexpected retained versions")
			}
		} else {
			return errors.New("unknown fixture mode")
		}
	}
	if mode == "cleanup" {
		result.ExactVersionsAbsent = true
		result.LaterVersionsPreserved = true
		result.DeleteMarkersAbsent = true
	}
	return writePrivateReceipt(output, result)
}
func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func writePrivateReceipt(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err = json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	return f.Sync()
}

func validateFixtureDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Path != "/routex_acceptance" || u.RawPath != "" || u.Fragment != "" || u.Port() == "" || u.User == nil || u.User.Username() != "routex_acceptance" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return errors.New("not dedicated database")
	}
	query, err := url.ParseQuery(u.RawQuery)
	password, _ := u.User.Password()
	if err != nil || len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" || len(password) < 16 {
		return errors.New("not dedicated database")
	}
	return nil
}
