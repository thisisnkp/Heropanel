package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/thisisnkp/heropanel/pkg/errx"
)

// An S3-compatible target with hand-rolled SigV4.
//
// Why not an SDK: the panel's standing rule is a lean dependency surface, and
// the three verbs a backup target needs — PUT, GET, DELETE an object — are a
// few hundred lines of stdlib against a written spec. SigV4 is stable and
// documented; an SDK would bring a dependency tree for the 1 % of it used here.
// "S3-compatible" covers AWS, Cloudflare R2, Backblaze B2 and MinIO alike.
//
// Uploads use UNSIGNED-PAYLOAD (the standard streaming choice, safe over TLS):
// the alternative is buffering or double-reading multi-gigabyte archives to
// pre-hash them. The payload is already AEAD-sealed by blobcrypt before it gets
// here, so integrity does not rest on the transport hash.

// S3Config locates the bucket and signs requests.
type S3Config struct {
	Endpoint  string // e.g. https://s3.amazonaws.com or http://127.0.0.1:9000
	Region    string // e.g. us-east-1
	Bucket    string
	AccessKey string
	SecretKey string
}

// S3Target implements Target against any S3-compatible endpoint, path-style
// (endpoint/bucket/key — the form MinIO and R2 accept without DNS games).
type S3Target struct {
	cfg    S3Config
	client *http.Client
	now    func() time.Time
}

// NewS3 builds the target. Returns nil when the config is incomplete, which the
// caller treats as "no s3 target configured".
func NewS3(cfg S3Config) *S3Target {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &S3Target{cfg: cfg, client: &http.Client{Timeout: 10 * time.Minute}, now: time.Now}
}

func (s *S3Target) Name() string { return TargetS3 }

const unsignedPayload = "UNSIGNED-PAYLOAD"

// emptySHA256 is the hash of an empty payload, used for GET/DELETE.
var emptySHA256 = hex.EncodeToString(func() []byte { h := sha256.Sum256(nil); return h[:] }())

func (s *S3Target) objectURL(key string) string {
	return strings.TrimRight(s.cfg.Endpoint, "/") + "/" + s.cfg.Bucket + "/" + key
}

// sign adds x-amz-date, x-amz-content-sha256 and the SigV4 Authorization header.
func (s *S3Target) sign(req *http.Request, payloadHash string) {
	t := s.now().UTC()
	amzDate := t.Format("20060102T150405Z")
	date := t.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalHeaders := "host:" + req.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonical := strings.Join([]string{
		req.Method, canonicalURI, req.URL.RawQuery,
		canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonical))

	scope := date + "/" + s.cfg.Region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256", amzDate, scope, hex.EncodeToString(canonicalHash[:]),
	}, "\n")

	mac := func(key, data []byte) []byte {
		h := hmac.New(sha256.New, key)
		h.Write(data)
		return h.Sum(nil)
	}
	kDate := mac([]byte("AWS4"+s.cfg.SecretKey), []byte(date))
	kRegion := mac(kDate, []byte(s.cfg.Region))
	kService := mac(kRegion, []byte("s3"))
	kSigning := mac(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(mac(kSigning, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKey, scope, signedHeaders, signature))
}

func (s *S3Target) do(ctx context.Context, method, key string, body io.Reader, size int64, payloadHash string) (*http.Response, error) {
	u, err := url.Parse(s.objectURL(key))
	if err != nil {
		return nil, errx.Internal(err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errx.Internal(err)
	}
	if size > 0 {
		req.ContentLength = size
	}
	s.sign(req, payloadHash)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, errx.Upstream(err, "s3_unreachable", "The S3 endpoint could not be reached.")
	}
	return resp, nil
}

// EnsureBucket creates the bucket if it does not exist. Idempotent: 200 is a
// fresh bucket, 409 is "already exists" (owned by us or not — a bucket someone
// else owns will fail loudly on the first Put, which is the honest failure
// point). Called best-effort at startup so "the bucket was never created" — the
// most common S3 misconfiguration — surfaces before the first scheduled backup
// silently piles up errors.
func (s *S3Target) EnsureBucket(ctx context.Context) error {
	u := strings.TrimRight(s.cfg.Endpoint, "/") + "/" + s.cfg.Bucket
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, nil)
	if err != nil {
		return errx.Internal(err)
	}
	s.sign(req, emptySHA256)
	resp, err := s.client.Do(req)
	if err != nil {
		return errx.Upstream(err, "s3_unreachable", "The S3 endpoint could not be reached.")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return errx.New(errx.KindUpstream, "s3_bucket_failed",
			fmt.Sprintf("Could not create the S3 bucket: HTTP %d.", resp.StatusCode))
	}
	return nil
}

// Put uploads a sealed archive.
func (s *S3Target) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	resp, err := s.do(ctx, http.MethodPut, key, r, size, unsignedPayload)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errx.New(errx.KindUpstream, "s3_put_failed",
			fmt.Sprintf("The S3 upload failed: HTTP %d.", resp.StatusCode))
	}
	return nil
}

// Get fetches a sealed archive. The caller closes the returned body.
func (s *S3Target) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.do(ctx, http.MethodGet, key, nil, 0, emptySHA256)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errx.New(errx.KindUpstream, "s3_get_failed",
			fmt.Sprintf("The S3 download failed: HTTP %d.", resp.StatusCode))
	}
	return resp.Body, nil
}

// Delete removes a sealed archive. A missing object is success — idempotent.
func (s *S3Target) Delete(ctx context.Context, key string) error {
	resp, err := s.do(ctx, http.MethodDelete, key, nil, 0, emptySHA256)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return errx.New(errx.KindUpstream, "s3_delete_failed",
			fmt.Sprintf("The S3 delete failed: HTTP %d.", resp.StatusCode))
	}
	return nil
}
