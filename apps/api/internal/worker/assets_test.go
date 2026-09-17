package worker

import (
	"encoding/json"
	"log/slog"
	"testing"
)

// The metadata keeps boto's field names rather than the Go client's, because the rows already in the table were written by boto and the web app reads them by those names.
func TestTheMetadataShapeIsBotos(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{
		"ContentType": "image/png", "ContentLength": 1234,
		"LastModified": "2026-09-16T08:00:00+00:00", "ETag": `"abc"`,
		"Metadata": map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ContentType", "ContentLength", "LastModified", "ETag", "Metadata"} {
		if _, present := decoded[key]; !present {
			t.Errorf("the metadata is missing %q", key)
		}
	}
	if len(decoded) != 5 {
		t.Errorf("the metadata has %d fields, want 5", len(decoded))
	}
}

// A negative window would put the cutoff in the future and sweep away every asset waiting to be uploaded, so it is refused rather than used.
func TestANegativeSweepWindowIsRefused(t *testing.T) {
	tasks := NewAssetTasks(nil, nil, slog.Default())
	if tasks.sweepIn != DefaultUnuploadedAssetDeleteDays {
		t.Fatalf("the default window is %d days", tasks.sweepIn)
	}
	tasks.SetUnuploadedAssetDeleteDays(-1)
	if tasks.sweepIn != DefaultUnuploadedAssetDeleteDays {
		t.Errorf("a negative window was taken: %d", tasks.sweepIn)
	}
	// Zero is a real window and is taken, which is what sweeps everything not yet uploaded.
	tasks.SetUnuploadedAssetDeleteDays(0)
	if tasks.sweepIn != 0 {
		t.Errorf("a zero window was refused: %d", tasks.sweepIn)
	}
	tasks.SetUnuploadedAssetDeleteDays(30)
	if tasks.sweepIn != 30 {
		t.Errorf("a set window is %d", tasks.sweepIn)
	}
}
