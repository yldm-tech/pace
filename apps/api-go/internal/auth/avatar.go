package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const defaultAvatarSizeLimit int64 = 5 * 1024 * 1024

type AvatarUpload struct {
	ObjectName      string
	AttributeName   string
	ContentType     string
	Size            int64
	StorageMetadata JSONValue
}

type AvatarStore interface {
	Upload(ctx context.Context, provider, avatarURL string) (*AvatarUpload, error)
	Delete(ctx context.Context, objectName string) error
}

type S3AvatarStore struct {
	client  *s3.Client
	bucket  string
	maxSize int64
	http    *http.Client
}

type AvatarStoreSettings struct {
	AccessKey        string
	SecretKey        string
	Region           string
	Bucket           string
	Endpoint         string
	UseMinio         bool
	MinioEndpointSSL bool
	MaxSize          int64
}

func NewS3AvatarStore(ctx context.Context, settings AvatarStoreSettings) (*S3AvatarStore, error) {
	if strings.TrimSpace(settings.Bucket) == "" {
		return nil, nil
	}
	region := settings.Region
	if region == "" {
		region = "us-east-1"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if settings.AccessKey != "" || settings.SecretKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(settings.AccessKey, settings.SecretKey, ""),
		))
	}
	if settings.Endpoint != "" {
		loadOptions = append(loadOptions, awsconfig.WithBaseEndpoint(settings.Endpoint))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load object storage configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = settings.UseMinio || settings.Endpoint != ""
	})
	maxSize := settings.MaxSize
	if maxSize <= 0 {
		maxSize = defaultAvatarSizeLimit
	}
	return &S3AvatarStore{
		client: client, bucket: settings.Bucket, maxSize: maxSize,
		http: newSSRFProtectedHTTPClient(),
	}, nil
}

func (store *S3AvatarStore) Upload(ctx context.Context, provider, avatarURL string) (*AvatarUpload, error) {
	content, contentType, err := store.download(ctx, avatarURL)
	if err != nil {
		// Django deliberately falls back to the provider URL for every avatar
		// download/upload failure. Keep that behavior and avoid failing login.
		return nil, nil
	}
	extension := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/webp": "webp"}[contentType]
	if extension == "" {
		return nil, nil
	}
	objectName, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	objectName += "-user-avatar." + extension
	_, err = store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(store.bucket),
		Key:         aws.String(objectName),
		Body:        bytes.NewReader(content),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return nil, nil
	}
	metadata := JSONValue(fmt.Sprintf(`{"content_type":%q,"size":%d}`, contentType, len(content)))
	return &AvatarUpload{
		ObjectName: objectName, AttributeName: provider + "-avatar." + extension,
		ContentType: contentType, Size: int64(len(content)), StorageMetadata: metadata,
	}, nil
}

func (store *S3AvatarStore) Delete(ctx context.Context, objectName string) error {
	if strings.TrimSpace(objectName) == "" {
		return nil
	}
	_, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(objectName)})
	return err
}

func (store *S3AvatarStore) download(ctx context.Context, rawURL string) ([]byte, string, error) {
	current, err := validateAvatarURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	for redirects := 0; redirects <= 5; redirects++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, "", err
		}
		response, err := store.http.Do(request)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			location := response.Header.Get("Location")
			response.Body.Close()
			if location == "" || redirects == 5 {
				return nil, "", fmt.Errorf("avatar redirect is invalid")
			}
			next, err := current.Parse(location)
			if err != nil {
				return nil, "", err
			}
			current, err = validateAvatarURL(next.String())
			if err != nil {
				return nil, "", err
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, "", fmt.Errorf("avatar response status %d", response.StatusCode)
		}
		defer response.Body.Close()
		if contentLength, err := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64); err == nil && contentLength > store.maxSize {
			return nil, "", fmt.Errorf("avatar is too large")
		}
		contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil {
			contentType = response.Header.Get("Content-Type")
		}
		if _, ok := map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}[contentType]; !ok {
			return nil, "", fmt.Errorf("unsupported avatar content type")
		}
		body := io.LimitReader(response.Body, store.maxSize+1)
		content, err := io.ReadAll(body)
		if err != nil {
			return nil, "", err
		}
		if int64(len(content)) > store.maxSize {
			return nil, "", fmt.Errorf("avatar is too large")
		}
		return content, contentType, nil
	}
	return nil, "", fmt.Errorf("avatar redirects exceeded limit")
}

func validateAvatarURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return nil, fmt.Errorf("avatar URL is invalid")
	}
	return parsed, nil
}

func newSSRFProtectedHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// Never use an ambient HTTP proxy here: proxying the request would move
		// SSRF validation to an untrusted intermediary.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range ips {
				if isPublicIP(resolved.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("avatar host resolves to a private address")
		},
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true,
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

func isPublicIP(ip net.IP) bool {
	return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}
