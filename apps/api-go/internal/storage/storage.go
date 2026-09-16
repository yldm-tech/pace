// Package storage signs the S3 requests the browser performs directly, which is how an attachment reaches the bucket without passing through the API.
package storage

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Settings mirrors the environment plane.settings.storage.S3Storage reads.
type Settings struct {
	AccessKey        string
	SecretKey        string
	Region           string
	Bucket           string
	Endpoint         string
	UseMinio         bool
	MinioEndpointSSL bool
	// PublicEndpoint is the host a presigned link is signed against when MinIO is in use. Django builds it out of WEB_URL rather than the endpoint the upload went to, because the browser reaches the bucket through the web host and not through the internal one.
	PublicEndpoint string
	// SignedURLExpiry is SIGNED_URL_EXPIRATION, an hour when unset.
	SignedURLExpiry time.Duration
}

// Store signs uploads and downloads. minio-go is used rather than the AWS SDK because the SDK has no presigned POST: it signs GET and PUT, and the browser upload here is a POST against a policy document.
type Store struct {
	client *minio.Client
	// publicClient signs the links a browser follows. It is the same client unless MinIO is in use, where a link has to be signed against the host the browser can reach.
	publicClient *minio.Client
	bucket       string
	expiry       time.Duration
}

// UploadTarget is the upload_data the create route returns, which the web client turns into a multipart form and posts straight at the bucket.
type UploadTarget struct {
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields"`
}

// New returns nil when no bucket is configured, which is how the rest of the API asks whether object storage is available at all.
func New(settings Settings) (*Store, error) {
	if strings.TrimSpace(settings.Bucket) == "" {
		return nil, nil
	}
	endpoint, secure, err := endpointAndScheme(settings)
	if err != nil {
		return nil, err
	}
	region := settings.Region
	if region == "" {
		region = "us-east-1"
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, err
	}
	expiry := settings.SignedURLExpiry
	if expiry <= 0 {
		expiry = time.Hour
	}
	store := &Store{client: client, publicClient: client, bucket: settings.Bucket, expiry: expiry}
	if settings.UseMinio && strings.TrimSpace(settings.PublicEndpoint) != "" {
		host, secure, err := endpointAndScheme(Settings{Endpoint: settings.PublicEndpoint, MinioEndpointSSL: settings.MinioEndpointSSL, Region: settings.Region})
		if err != nil {
			return nil, err
		}
		public, err := minio.New(host, &minio.Options{
			Creds:  credentials.NewStaticV4(settings.AccessKey, settings.SecretKey, ""),
			Secure: secure,
			Region: region,
		})
		if err != nil {
			return nil, err
		}
		store.publicClient = public
	}
	return store, nil
}

// endpointAndScheme splits the configured endpoint into the host and scheme minio-go wants. An unset endpoint means real S3.
func endpointAndScheme(settings Settings) (string, bool, error) {
	raw := strings.TrimSpace(settings.Endpoint)
	if raw == "" {
		return "s3." + regionOrDefault(settings.Region) + ".amazonaws.com", true, nil
	}
	if !strings.Contains(raw, "://") {
		// A bare host keeps the scheme MinIO's own flag decides.
		return raw, settings.MinioEndpointSSL, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	if parsed.Host == "" {
		return "", false, errors.New("object storage endpoint has no host")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func regionOrDefault(region string) string {
	if region == "" {
		return "us-east-1"
	}
	return region
}

// PresignedUpload is generate_presigned_post: a policy the browser posts against, restricted to one key, one content type and a size range.
func (store *Store) PresignedUpload(ctx context.Context, objectName, fileType string, fileSize int64) (UploadTarget, error) {
	policy := minio.NewPostPolicy()
	if err := policy.SetBucket(store.bucket); err != nil {
		return UploadTarget{}, err
	}
	if err := policy.SetKey(objectName); err != nil {
		return UploadTarget{}, err
	}
	if err := policy.SetExpires(time.Now().UTC().Add(store.expiry)); err != nil {
		return UploadTarget{}, err
	}
	if err := policy.SetContentType(fileType); err != nil {
		return UploadTarget{}, err
	}
	// Django's lower bound is one byte, which is what rejects an empty upload.
	if err := policy.SetContentLengthRange(1, fileSize); err != nil {
		return UploadTarget{}, err
	}
	signed, fields, err := store.client.PresignedPostPolicy(ctx, policy)
	if err != nil {
		return UploadTarget{}, err
	}
	return UploadTarget{URL: signed.String(), Fields: fields}, nil
}

// PresignedDownload is generate_presigned_url with a content disposition, which is what makes the browser save an attachment under its original name rather than render it.
func (store *Store) PresignedDownload(ctx context.Context, objectName, disposition, filename string) (string, error) {
	values := url.Values{}
	values.Set("response-content-disposition", contentDisposition(disposition, filename))
	signed, err := store.client.PresignedGetObject(ctx, store.bucket, objectName, store.expiry, values)
	if err != nil {
		return "", err
	}
	return signed.String(), nil
}

// contentDisposition is S3Storage._get_content_disposition. The filename goes through the RFC 5987 form, so a name with spaces or non-ASCII characters survives the round trip.
func contentDisposition(disposition, filename string) string {
	if filename == "" {
		return disposition
	}
	return disposition + "; filename*=UTF-8''" + quote(filename)
}

// quote is Python's urllib.parse.quote with its default safe set, which leaves the unreserved characters and the slash alone and percent-encodes the rest byte by byte.
func quote(value string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-~/"
	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if strings.IndexByte(unreserved, character) >= 0 {
			builder.WriteByte(character)
			continue
		}
		builder.WriteByte('%')
		builder.WriteString(strings.ToUpper(strconv.FormatUint(uint64(character), 16)))
	}
	return builder.String()
}

// ObjectMetadata is what get_object_metadata reports back, with the keys boto's head_object uses rather than the ones the Go client does.
type ObjectMetadata struct {
	ContentType   string            `json:"ContentType"`
	ContentLength int64             `json:"ContentLength"`
	LastModified  *string           `json:"LastModified"`
	ETag          string            `json:"ETag"`
	Metadata      map[string]string `json:"Metadata"`
}

// StatObject is get_object_metadata: the object's headers, read without fetching it.
//
// The shape is boto's rather than this client's, because the rows already in the table were written by boto and the web app reads them by those names. The etag is quoted for the same reason: boto reports the header as it arrived, quotes included, and this client strips them.
func (store *Store) StatObject(ctx context.Context, objectName string) (*ObjectMetadata, error) {
	info, err := store.client.StatObject(ctx, store.bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	for key, value := range info.UserMetadata {
		// boto lowercases what it strips the x-amz-meta- prefix from.
		metadata[strings.ToLower(key)] = value
	}
	var lastModified *string
	if !info.LastModified.IsZero() {
		// Python renders an aware datetime with a colon in the offset.
		rendered := info.LastModified.UTC().Format("2006-01-02T15:04:05-07:00")
		lastModified = &rendered
	}
	etag := info.ETag
	if etag != "" && !strings.HasPrefix(etag, `"`) {
		etag = `"` + etag + `"`
	}
	return &ObjectMetadata{
		ContentType: info.ContentType, ContentLength: info.Size,
		LastModified: lastModified, ETag: etag, Metadata: metadata,
	}, nil
}

// PutObject writes bytes straight into the bucket, which is how the worker puts a finished export there without a browser in the middle.
//
// publicRead is the ACL Django sets on the MinIO path and leaves off everywhere else, which is what makes the object readable without a signature on a local install.
func (store *Store) PutObject(ctx context.Context, objectName, contentType string, payload []byte, publicRead bool) error {
	options := minio.PutObjectOptions{ContentType: contentType}
	if publicRead {
		options.UserMetadata = map[string]string{"x-amz-acl": "public-read"}
	}
	_, err := store.client.PutObject(ctx, store.bucket, objectName, bytes.NewReader(payload), int64(len(payload)), options)
	return err
}

// PresignedObject is generate_presigned_url with nothing but an expiry, which is the link an export ends up as. It is signed against the public host so a browser can follow it.
func (store *Store) PresignedObject(ctx context.Context, objectName string, expiry time.Duration) (string, error) {
	signed, err := store.publicClient.PresignedGetObject(ctx, store.bucket, objectName, expiry, url.Values{})
	if err != nil {
		return "", err
	}
	return signed.String(), nil
}

// RemoveObject takes an object out of the bucket. The exporter sweep uses it on the spreadsheets whose links have expired.
func (store *Store) RemoveObject(ctx context.Context, objectName string) error {
	return store.client.RemoveObject(ctx, store.bucket, objectName, minio.RemoveObjectOptions{})
}

// CopyObject is copy_object: the bucket copies the bytes from one key to another without them passing through here.
func (store *Store) CopyObject(ctx context.Context, sourceKey, destinationKey string) error {
	_, err := store.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: store.bucket, Object: destinationKey},
		minio.CopySrcOptions{Bucket: store.bucket, Object: sourceKey})
	return err
}
