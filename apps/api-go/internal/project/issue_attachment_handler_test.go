package project

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// The mime allowlist is generated from settings.ATTACHMENT_MIME_TYPES, where two entries are listed twice.
func TestAttachmentMimeAllowlist(t *testing.T) {
	if len(attachmentMimeTypes) != 71 {
		t.Fatalf("allowlist has %d entries, want 71", len(attachmentMimeTypes))
	}
	for _, allowed := range []string{"application/pdf", "image/png", "text/markdown", "video/mp4", "application/zip"} {
		if !attachmentMimeTypes[allowed] {
			t.Errorf("%q should be allowed", allowed)
		}
	}
	for _, refused := range []string{"application/x-msdownload", "text/html", "application/x-sh", "", "image/png; charset=utf-8"} {
		if attachmentMimeTypes[refused] {
			t.Errorf("%q should be refused", refused)
		}
	}
}

// The size is clamped to the configured limit, and an absent size means the limit itself.
func TestAttachmentSizeIsClampedToTheLimit(t *testing.T) {
	handler := &Handler{settings: Settings{FileSizeLimit: 1000}}
	if got := handler.fileSizeLimit(); got != 1000 {
		t.Fatalf("limit = %d", got)
	}
	// An unset limit falls back to Django's five megabytes.
	bare := &Handler{}
	if got := bare.fileSizeLimit(); got != 5242880 {
		t.Fatalf("default limit = %d", got)
	}
	for _, test := range []struct {
		raw  string
		want int64
		ok   bool
	}{
		{raw: "512", want: 512, ok: true},
		{raw: `"512"`, want: 512, ok: true},
		{raw: "0", want: 0, ok: true},
		{raw: "-1", want: -1, ok: true},
		// int() refuses a fractional string, and so does this.
		{raw: "1.5", ok: false},
		{raw: `"abc"`, ok: false},
		{raw: "null", ok: false},
	} {
		got, ok := parseAttachmentSize(json.RawMessage(test.raw))
		if ok != test.ok || (ok && got != test.want) {
			t.Errorf("parseAttachmentSize(%s) = (%d, %v), want (%d, %v)", test.raw, got, ok, test.want, test.ok)
		}
	}
}

// IssueAttachmentSerializer is fields = "__all__" plus the asset_url property, so every column of the row is in the response.
func TestAttachmentSerializationCarriesEveryColumn(t *testing.T) {
	entity := attachmentEntityType
	project, issue := "project-id", "issue-id"
	asset := FileAsset{
		ID: "asset-id", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		Asset: "workspace-id/abc-report.pdf", EntityType: &entity,
		ProjectID: &project, IssueID: &issue, Size: 1024,
		Attributes: auth.JSONValue([]byte(`{"name":"report.pdf"}`)),
	}
	data := attachmentJSON(asset, "acme")
	for _, field := range []string{
		"id", "created_at", "updated_at", "deleted_at", "attributes", "asset",
		"entity_type", "entity_identifier", "is_deleted", "is_archived",
		"external_id", "external_source", "size", "is_uploaded", "storage_metadata",
		"created_by", "updated_by", "user", "workspace", "draft_issue",
		"project", "issue", "comment", "page", "asset_url",
	} {
		if _, present := data[field]; !present {
			t.Errorf("serialized attachment is missing %q", field)
		}
	}
	if len(data) != 25 {
		t.Fatalf("serialized attachment has %d fields, want 25", len(data))
	}
	if data["asset_url"] != "/api/assets/v2/workspaces/acme/projects/project-id/issues/issue-id/attachments/asset-id/" {
		t.Fatalf("asset_url = %v", data["asset_url"])
	}
}

// The metadata task runs only when the row has nothing yet, and Django's default is an empty object rather than null.
func TestEmptyStorageMetadataReadsAsMissing(t *testing.T) {
	for _, raw := range []string{"", "null", "{}", "  {}  "} {
		if (FileAsset{StorageMetadata: auth.JSONValue(raw)}).HasStorageMetadata() {
			t.Errorf("%q should read as missing", raw)
		}
	}
	if !(FileAsset{StorageMetadata: auth.JSONValue(`{"ETag":"x"}`)}).HasStorageMetadata() {
		t.Error("a populated metadata object should read as present")
	}
}

// The object key is the workspace prefix, an unguessable suffix and the sanitized name, so a caller cannot steer it anywhere else.
func TestObjectKeysStayUnderTheWorkspacePrefix(t *testing.T) {
	for _, name := range []string{"../../etc/passwd", `..\..\windows`, "/abs.txt", "ok.pdf"} {
		key := "workspace-id/" + "abcdef" + "-" + sanitizeFilename(name)
		if strings.Count(key, "/") != 1 {
			t.Errorf("key %q for name %q leaves the workspace prefix", key, name)
		}
	}
}

func TestAttributeEncodingLeavesMarkupAlone(t *testing.T) {
	// Django stores the name verbatim; Go's default encoder would escape the angle brackets.
	encoded, err := marshalUnescaped(map[string]any{"name": "a<b>c.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "a<b>c.txt") {
		t.Fatalf("attributes = %s, want the name unescaped", encoded)
	}
}
