package project

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// An unnamed scale is given ten lowercase letters, which is the one place in this API that names something for the caller.
func TestAnUnnamedEstimateIsNamedAtRandom(t *testing.T) {
	seen := map[string]bool{}
	for attempt := 0; attempt < 20; attempt++ {
		name := randomEstimateName()
		if len(name) != 10 {
			t.Fatalf("the name is %d letters, want 10", len(name))
		}
		if strings.ToLower(name) != name {
			t.Errorf("the name %q is not lowercase", name)
		}
		for _, letter := range name {
			if letter < 'a' || letter > 'z' {
				t.Errorf("the name %q holds something other than a letter", name)
			}
		}
		seen[name] = true
	}
	if len(seen) < 2 {
		t.Error("twenty names came out the same, which is not a random one")
	}
}

// The read shape nests the points inside the scale; the point shape is twelve fields of its own.
func TestTheEstimateShapes(t *testing.T) {
	point := EstimatePoint{ID: "point-id", Key: 0, Value: "1"}
	body := estimatePointJSON(point)
	if len(body) != 12 {
		t.Fatalf("a point has %d fields, want 12", len(body))
	}
	// The key is an integer and the value a string, which is what keeps a scale of 1/2/3 from rendering as numbers.
	if _, isInt := body["key"].(int); !isInt {
		t.Errorf("the key is %T, want an integer", body["key"])
	}
	if _, isString := body["value"].(string); !isString {
		t.Errorf("the value is %T, want a string", body["value"])
	}

	read := estimateReadJSON(Estimate{ID: "estimate-id"}, []EstimatePoint{point})
	// Thirteen: the twelve the scale carries plus the points nested inside it.
	if len(read) != 13 {
		t.Fatalf("a scale has %d fields, want 13", len(read))
	}
	points, ok := read["points"].([]map[string]any)
	if !ok {
		if converted, isGin := read["points"].([]gin.H); isGin {
			points = make([]map[string]any, len(converted))
			for index, value := range converted {
				points[index] = value
			}
		} else {
			t.Fatalf("the points are %T", read["points"])
		}
	}
	if len(points) != 1 {
		t.Fatalf("the scale carries %d points, want 1", len(points))
	}
	// A scale with no points reports an empty list rather than a null.
	empty := estimateReadJSON(Estimate{ID: "estimate-id"}, nil)
	if converted, isGin := empty["points"].([]gin.H); !isGin || len(converted) != 0 {
		t.Errorf("an empty scale reports %v", empty["points"])
	}
}

// A key of zero is refused even though zero is where a scale starts, because both fields are tested for truth rather than for presence.
func TestAKeyOfZeroIsRefused(t *testing.T) {
	refused := func(key float64, keyGiven bool, value string, valueGiven bool) bool {
		return !keyGiven || key == 0 || !valueGiven || value == ""
	}
	if !refused(0, true, "1", true) {
		t.Error("a key of zero is accepted, and the check asks for a truthy one")
	}
	if !refused(1, true, "", true) {
		t.Error("an empty value is accepted")
	}
	if refused(1, true, "1", true) {
		t.Error("a whole point is refused")
	}
}
