// Package storage signs the S3 requests the browser performs directly, which is how an attachment reaches the bucket without passing through the API.
package storage

import (
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
	// SignedURLExpiry is SIGNED_URL_EXPIRATION, an hour when unset.
	SignedURLExpiry time.Duration
}

// Store signs uploads and downloads. minio-go is used rather than the AWS SDK because the SDK has no presigned POST: it signs GET and PUT, and the browser upload here is a POST against a policy document.
type Store struct {
	client *minio.Client
	bucket string
	expiry time.Duration
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
	return &Store{client: client, bucket: settings.Bucket, expiry: expiry}, nil
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

// CopyObject is copy_object: the bucket copies the bytes from one key to another without them passing through here.
func (store *Store) CopyObject(ctx context.Context, sourceKey, destinationKey string) error {
	_, err := store.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: store.bucket, Object: destinationKey},
		minio.CopySrcOptions{Bucket: store.bucket, Object: sourceKey})
	return err
}
