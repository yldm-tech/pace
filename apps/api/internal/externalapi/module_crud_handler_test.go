package externalapi

import (
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

// The module paths carry every method Django binds on them.
func TestTheModuleRoutesCarryEveryMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, Settings{}).Register(router)

	const base = "/api/v1/workspaces/:slug/projects/:project/modules/"
	wanted := map[string]bool{
		"GET " + base: false, "POST " + base: false,
		"GET " + base + ":module/": false, "PATCH " + base + ":module/": false,
		"DELETE " + base + ":module/":                      false,
		"GET " + base + ":module/module-issues/":           false,
		"POST " + base + ":module/module-issues/":          false,
		"GET " + base + ":module/module-issues/:issue/":    false,
		"DELETE " + base + ":module/module-issues/:issue/": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, listed := wanted[key]; listed {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Errorf("%s is not served", route)
		}
	}
}

// The members are on every module body, because the serializer adds them itself rather than declaring a field for them.
func TestAModuleAlwaysReportsItsMembers(t *testing.T) {
	row := moduleRow{Module: Module{ID: "module-id"}, MemberIDs: pq.StringArray{"member-id"}}
	counted := moduleJSON(row, true)
	if len(counted) != 29 {
		t.Fatalf("the annotated module has %d fields, want 29", len(counted))
	}
	plain := moduleJSON(row, false)
	if len(plain) != 23 {
		t.Fatalf("the plain module has %d fields, want 23", len(plain))
	}
	for _, data := range []gin.H{counted, plain} {
		members, ok := data["members"].([]string)
		if !ok || len(members) != 1 || members[0] != "member-id" {
			t.Errorf("the members are %v", data["members"])
		}
	}
	// An empty membership is a list rather than a null.
	empty := moduleJSON(moduleRow{Module: Module{ID: "module-id"}}, false)
	if members, ok := empty["members"].([]string); !ok || len(members) != 0 {
		t.Errorf("an empty membership renders as %v", empty["members"])
	}
}

// The two planning dates are days rather than instants, which is what a DateField renders.
func TestTheModuleDatesAreDays(t *testing.T) {
	day := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	data := moduleJSON(moduleRow{Module: Module{ID: "module-id", StartDate: &day, TargetDate: &day, ArchivedAt: &day}}, false)
	for _, field := range []string{"start_date", "target_date"} {
		if got, ok := data[field].(string); !ok || got != "2026-03-04" {
			t.Errorf("%s is %v, want the day alone", field, data[field])
		}
	}
	// The archive stamp is a DateTimeField here, unlike the work item's, so it keeps its time.
	if _, isString := data["archived_at"].(string); isString {
		t.Error("the archive stamp was cut down to a day, and on a module it is an instant")
	}
}

// The two refusals for a name that is taken are different shapes: four keys on the create and one on the update, both flat rather than the lists a field error carries.
func TestTheNameConflictShapes(t *testing.T) {
	create := gin.H{
		"id": "module-id", "code": "MODULE_NAME_ALREADY_EXISTS",
		"error": "Module with this name already exists", "message": "Module with this name already exists",
	}
	if len(create) != 4 {
		t.Fatalf("the create refusal has %d keys, want 4", len(create))
	}
	if _, isList := create["error"].([]string); isList {
		t.Error("the refusal wraps its message in a list, and a raise from create() does not")
	}
}

// The activity payload carries the repr of a queryset rather than a list, truncated the way Python truncates one.
func TestTheModuleActivityCarriesAQuerysetRepr(t *testing.T) {
	if got := querysetRepr(nil); got != "<QuerySet []>" {
		t.Errorf("an empty list renders as %s", got)
	}
	if got := querysetRepr([]string{"a", "b"}); got != "<QuerySet [UUID('a'), UUID('b')]>" {
		t.Errorf("two identifiers render as %s", got)
	}
	long := make([]string, 25)
	for index := range long {
		long[index] = "x"
	}
	rendered := querysetRepr(long)
	if !contains(rendered, "'...(remaining elements truncated)...'") {
		t.Errorf("a long list is not truncated: %s", rendered)
	}
	// Twenty entries and the truncation marker, and nothing after it.
	if count := countOccurrences(rendered, "UUID("); count != 20 {
		t.Errorf("the repr holds %d identifiers, want 20", count)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

func countOccurrences(haystack, needle string) int {
	count := 0
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			count++
		}
	}
	return count
}
