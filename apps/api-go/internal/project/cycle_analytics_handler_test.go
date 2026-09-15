package project

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yldm-tech/pace/apps/api-go/internal/drf"
)

// A stored distribution is returned as it was stored, and a key that is not in it defaults to the empty shape rather than to null.
func TestAStoredDistributionDefaultsKeyByKey(t *testing.T) {
	snapshot, ok := drf.DecodeJSON([]byte(`{"distribution": {"labels": [{"label_name": "bug"}]}}`)).(map[string]any)
	if !ok {
		t.Fatal("the snapshot did not decode to an object")
	}
	distribution, _ := snapshot["distribution"].(map[string]any)

	if encoded, _ := json.Marshal(snapshotList(distribution, "labels")); string(encoded) != `[{"label_name":"bug"}]` {
		t.Errorf("a stored list came back as %s", encoded)
	}
	if encoded, _ := json.Marshal(snapshotList(distribution, "assignees")); string(encoded) != "[]" {
		t.Errorf("a missing list came back as %s, want []", encoded)
	}
	if encoded, _ := json.Marshal(snapshotObject(distribution, "completion_chart")); string(encoded) != "{}" {
		t.Errorf("a missing chart came back as %s, want {}", encoded)
	}
}

// A snapshot with no distribution at all still answers three empty shapes rather than failing.
func TestAnEmptyDistributionStillAnswers(t *testing.T) {
	if encoded, _ := json.Marshal(snapshotList(nil, "labels")); string(encoded) != "[]" {
		t.Errorf("labels came back as %s, want []", encoded)
	}
	if encoded, _ := json.Marshal(snapshotObject(nil, "completion_chart")); string(encoded) != "{}" {
		t.Errorf("the chart came back as %s, want {}", encoded)
	}
}

// The three conditions the aggregates carry are the ones Django writes, and the completed and pending pair must stay complementary.
func TestTheDistributionFiltersStayComplementary(t *testing.T) {
	if completedIssues == pendingIssues {
		t.Fatal("completed and pending must not carry the same condition")
	}
	for _, shared := range []string{"i.archived_at IS NULL", "i.is_draft = FALSE"} {
		for name, condition := range map[string]string{"completed": completedIssues, "pending": pendingIssues, "live": liveIssue} {
			if !strings.Contains(condition, shared) {
				t.Errorf("the %s condition is missing %q", name, shared)
			}
		}
	}
	if !strings.Contains(completedIssues, "i.completed_at IS NOT NULL") || !strings.Contains(pendingIssues, "i.completed_at IS NULL") {
		t.Error("the completed and pending conditions must split on the completion time")
	}
	if strings.Contains(liveIssue, "completed_at") {
		t.Error("the total counts both halves, so it must not mention the completion time")
	}
}
