package storage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestForRequestSignsAgainstTheRequestHost pins ForRequest to S3Storage.__init__ in plane/settings/storage.py, which on MinIO builds its client with endpoint_url=f"{request.scheme}://{request.get_host()}" whenever a request is in hand.
//
// The endpoint the API writes through is a container name -- plane-minio:9000 in the shipped compose file -- so a presigned URL signed against it is one the browser cannot resolve. Every upload failed with ERR_NAME_NOT_RESOLVED until this existed.
func TestForRequestSignsAgainstTheRequestHost(t *testing.T) {
	store, err := New(Settings{
		AccessKey: "key", SecretKey: "secret", Bucket: "uploads",
		Endpoint: "http://plane-minio:9000", UseMinio: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/assets/", nil)
	request.Host = "plane.example.test"

	target, err := store.ForRequest(request).PresignedUpload(t.Context(), "an-object", "image/png", 1000)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.Contains(target.URL, "plane.example.test") {
		t.Errorf("upload signed against %q, want the request host", target.URL)
	}
	if strings.Contains(target.URL, "plane-minio") {
		t.Errorf("upload signed against the internal host, which no browser can resolve: %q", target.URL)
	}

	link, err := store.ForRequest(request).PresignedDownload(t.Context(), "an-object", "attachment", "a.png")
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if !strings.Contains(link, "plane.example.test") || strings.Contains(link, "plane-minio") {
		t.Errorf("download signed against %q, want the request host", link)
	}
}

// Without a request there is nothing to sign against, which is the background worker's case, and Django falls back to AWS_S3_ENDPOINT_URL there.
func TestForRequestWithoutARequestKeepsTheConfiguredEndpoint(t *testing.T) {
	store, err := New(Settings{
		AccessKey: "key", SecretKey: "secret", Bucket: "uploads",
		Endpoint: "http://plane-minio:9000", UseMinio: true,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if store.ForRequest(nil) != store {
		t.Error("ForRequest(nil) built a new store")
	}
}

// Off MinIO the endpoint is the real S3 one, and the host a request happens to arrive on must not be signed against.
func TestForRequestIsInertWithoutMinio(t *testing.T) {
	store, err := New(Settings{
		AccessKey: "key", SecretKey: "secret", Bucket: "uploads", Region: "eu-west-1",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/assets/", nil)
	request.Host = "plane.example.test"

	target, err := store.ForRequest(request).PresignedUpload(t.Context(), "an-object", "image/png", 1000)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if strings.Contains(target.URL, "plane.example.test") {
		t.Errorf("signed against the request host on real S3: %q", target.URL)
	}
	if !strings.Contains(target.URL, "amazonaws.com") {
		t.Errorf("signed against %q, want S3", target.URL)
	}
}
