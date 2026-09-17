package storage

import (
	"os"
	"strings"
	"testing"
)

// The filename in a content disposition goes through Python's quote, whose safe set is not Go's. The fixture is that function's output.
func TestFilenameQuotingMatchesPython(t *testing.T) {
	fixture, err := os.ReadFile("testdata/quote.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(fixture), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		input, want, found := strings.Cut(line, "\t")
		if !found {
			t.Fatalf("malformed fixture row %q", line)
		}
		rows++
		if got := quote(input); got != want {
			t.Errorf("quote(%q) = %q, want %q", input, got, want)
		}
	}
	if rows != 8 {
		t.Fatalf("fixture has %d rows, want 8", rows)
	}
}

func TestContentDispositionUsesTheEncodedForm(t *testing.T) {
	if got := contentDisposition("attachment", "my file.txt"); got != "attachment; filename*=UTF-8''my%20file.txt" {
		t.Fatalf("disposition = %q", got)
	}
	// Django falls back to the bare disposition when it has no filename.
	if got := contentDisposition("inline", ""); got != "inline" {
		t.Fatalf("disposition = %q", got)
	}
}

func TestNoBucketMeansNoStore(t *testing.T) {
	// The rest of the API asks whether object storage is configured by checking for a nil store, so an unset bucket must not be an error.
	store, err := New(Settings{})
	if store != nil || err != nil {
		t.Fatalf("New with no bucket gave (%v, %v), want (nil, nil)", store, err)
	}
	if store, err := New(Settings{Bucket: "   "}); store != nil || err != nil {
		t.Fatalf("New with a blank bucket gave (%v, %v)", store, err)
	}
}

func TestEndpointSplitting(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings Settings
		host     string
		secure   bool
	}{
		{name: "real S3 from the region", settings: Settings{Bucket: "b", Region: "eu-west-2"}, host: "s3.eu-west-2.amazonaws.com", secure: true},
		{name: "real S3 with no region", settings: Settings{Bucket: "b"}, host: "s3.us-east-1.amazonaws.com", secure: true},
		{name: "an http endpoint", settings: Settings{Bucket: "b", Endpoint: "http://minio:9000"}, host: "minio:9000", secure: false},
		{name: "an https endpoint", settings: Settings{Bucket: "b", Endpoint: "https://objects.example.com"}, host: "objects.example.com", secure: true},
		// A bare host carries no scheme, so MinIO's own flag decides it.
		{name: "a bare host, plain", settings: Settings{Bucket: "b", Endpoint: "minio:9000"}, host: "minio:9000", secure: false},
		{name: "a bare host, TLS", settings: Settings{Bucket: "b", Endpoint: "minio:9000", MinioEndpointSSL: true}, host: "minio:9000", secure: true},
	} {
		host, secure, err := endpointAndScheme(test.settings)
		if err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if host != test.host || secure != test.secure {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", test.name, host, secure, test.host, test.secure)
		}
	}
}
