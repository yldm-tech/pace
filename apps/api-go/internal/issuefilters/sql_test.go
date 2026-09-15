package issuefilters

import (
	"strings"
	"testing"
	"time"
)

func sqlFor(t *testing.T, params map[string]string, method string) (joins, conditions []string, arguments []any) {
	t.Helper()
	filters := Parse(params, method, "", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC))
	joins, conditions, arguments, ok := SQL(filters)
	if !ok {
		t.Fatalf("issueFilterSQL refused %v", params)
	}
	return joins, conditions, arguments
}

// A value lookup on a multi-valued relation joins with an INNER, and its soft-delete companion sits on the same join rather than a second one.
func TestAValueLookupAndItsSoftDeleteCompanionShareOneJoin(t *testing.T) {
	joins, conditions, arguments := sqlFor(t, map[string]string{"labels": "00000000-0000-0000-0000-000000000001"}, "GET")
	if len(joins) != 1 {
		t.Fatalf("joins = %v, want one", joins)
	}
	if !strings.HasPrefix(joins[0], "JOIN issue_labels flab ") {
		t.Fatalf("join = %q, want an inner join on the through table", joins[0])
	}
	// The value lookup reaches only the through table; Django never joins the labels table itself for this.
	joined := strings.Join(conditions, " AND ")
	if !strings.Contains(joined, "flab.label_id IN (?)") || !strings.Contains(joined, "flab.deleted_at IS NULL") {
		t.Fatalf("conditions = %v", conditions)
	}
	if len(arguments) != 1 {
		t.Fatalf("arguments = %v", arguments)
	}
}

// A null value lookup forces a LEFT OUTER join, because an inner one can never produce the row it is looking for. The soft-delete companion on its own does not.
func TestOnlyANullValueLookupWidensTheJoin(t *testing.T) {
	joins, _, _ := sqlFor(t, map[string]string{"labels": "None"}, "GET")
	if len(joins) != 1 || !strings.HasPrefix(joins[0], "LEFT JOIN issue_labels ") {
		t.Fatalf("joins = %v, want a left join", joins)
	}
	// An empty value writes only the soft-delete companion, which does not widen anything.
	inner, _, _ := sqlFor(t, map[string]string{"labels": ""}, "GET")
	if len(inner) != 1 || !strings.HasPrefix(inner[0], "JOIN issue_labels ") {
		t.Fatalf("joins = %v, want an inner join", inner)
	}
}

// Asking for both the literal None and a specific label puts two conditions on one join, and a column cannot be null and in a list at once, so the filter matches nothing. Reproduced rather than corrected.
func TestNoneTogetherWithAValueMatchesNothing(t *testing.T) {
	_, conditions, _ := sqlFor(t, map[string]string{"labels": "None,00000000-0000-0000-0000-000000000001"}, "GET")
	joined := strings.Join(conditions, " AND ")
	if !strings.Contains(joined, "flab.label_id IN (?)") || !strings.Contains(joined, "flab.label_id IS NULL") {
		t.Fatalf("conditions = %v, want both on the same column", conditions)
	}
}

// Each relation family gets its own join.
func TestEachFamilyGetsItsOwnJoin(t *testing.T) {
	joins, _, _ := sqlFor(t, map[string]string{
		"labels":     "00000000-0000-0000-0000-000000000001",
		"assignees":  "00000000-0000-0000-0000-000000000002",
		"module":     "00000000-0000-0000-0000-000000000003",
		"cycle":      "00000000-0000-0000-0000-000000000004",
		"subscriber": "00000000-0000-0000-0000-000000000005",
	}, "GET")
	if len(joins) != 5 {
		t.Fatalf("joins = %v, want one per family", joins)
	}
	aliases := map[string]bool{}
	for _, join := range joins {
		// The alias is the token just before ON, which is where it sits for both join kinds.
		parts := strings.Fields(join)
		for index, part := range parts {
			if part == "ON" {
				aliases[parts[index-1]] = true
				break
			}
		}
	}
	if len(aliases) != 5 {
		t.Fatalf("aliases collide: %v", joins)
	}
}

// The plain columns need no join at all.
func TestPlainColumnsNeedNoJoin(t *testing.T) {
	joins, conditions, arguments := sqlFor(t, map[string]string{"priority": "high,urgent", "name": "bug"}, "GET")
	if len(joins) != 0 {
		t.Fatalf("joins = %v, want none", joins)
	}
	joined := strings.Join(conditions, " AND ")
	if !strings.Contains(joined, "i.priority IN (?, ?)") {
		t.Fatalf("conditions = %v", conditions)
	}
	if !strings.Contains(joined, "UPPER(i.name::text) LIKE UPPER(?)") {
		t.Fatalf("conditions = %v", conditions)
	}
	// The conditions are emitted in lookup-name order, so name__icontains comes before priority__in.
	if len(arguments) != 3 || arguments[0] != "%bug%" {
		t.Fatalf("arguments = %v", arguments)
	}
}

// A caller's own wildcards are neutralised so they match themselves, which is what Django's prep_for_like_query does.
func TestLikePatternsAreEscaped(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{in: "a%b_c", want: `a\%b\_c`},
		{in: `back\slash`, want: `back\\slash`},
		{in: "plain", want: "plain"},
	} {
		if got := escapeLikePattern(test.in); got != test.want {
			t.Errorf("escapeLikePattern(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// The two DateFields compare directly; the three DateTimeFields go through a cast to the deployment's timezone first.
func TestDateLookupsUseTheRightCast(t *testing.T) {
	_, conditions, _ := sqlFor(t, map[string]string{"target_date": "2026-01-02;after"}, "GET")
	if len(conditions) != 1 || conditions[0] != "i.target_date >= ?" {
		t.Fatalf("conditions = %v, want a direct comparison", conditions)
	}
	_, timestamps, _ := sqlFor(t, map[string]string{"created_at": "2026-01-02;after"}, "GET")
	if len(timestamps) != 1 || !strings.Contains(timestamps[0], "AT TIME ZONE 'UTC')::date >=") {
		t.Fatalf("conditions = %v, want the timezone cast", timestamps)
	}
}

// logged_by exists nowhere but in issue_filters itself, so Django cannot resolve it and answers 500. The translation refuses rather than inventing a column.
func TestLoggedByIsRefused(t *testing.T) {
	filters := Parse(map[string]string{"logged_by": "00000000-0000-0000-0000-000000000001"}, "GET", "", time.Now())
	if _, _, _, ok := SQL(filters); ok {
		t.Fatal("logged_by should be refused, since the ORM cannot resolve it either")
	}
}

// A POST hands the value over whole rather than as a list, and the IN lookup takes it as a single element.
func TestPostValuesBecomeSingleElementLists(t *testing.T) {
	_, conditions, arguments := sqlFor(t, map[string]string{"priority": "high,urgent"}, "POST")
	if len(conditions) != 1 || conditions[0] != "i.priority IN (?)" {
		t.Fatalf("conditions = %v", conditions)
	}
	if len(arguments) != 1 || arguments[0] != "high,urgent" {
		t.Fatalf("arguments = %v, want the value whole", arguments)
	}
}

// An empty list matches nothing rather than everything.
func TestAnEmptyListMatchesNothing(t *testing.T) {
	condition, _ := filterCondition(issueFilterSQLMap["priority__in"], ListValue(nil))
	if condition != "FALSE" {
		t.Fatalf("an empty IN rendered as %q", condition)
	}
}

// Every lookup the filter mapping can produce has to be translatable, or a caller can reach a 500 the port did not intend.
func TestEveryProducibleLookupIsMapped(t *testing.T) {
	values := map[string][]string{
		"state":       {"00000000-0000-0000-0000-000000000001", "None"},
		"state_group": {"backlog"}, "estimate_point": {"1"}, "priority": {"high"},
		"parent":        {"00000000-0000-0000-0000-000000000001", "None"},
		"labels":        {"00000000-0000-0000-0000-000000000001", "None"},
		"assignees":     {"00000000-0000-0000-0000-000000000001", "None"},
		"mentions":      {"00000000-0000-0000-0000-000000000001"},
		"created_by":    {"00000000-0000-0000-0000-000000000001", "None"},
		"name":          {"x"},
		"created_at":    {"2026-01-02;after", "2026-01-02;before", "2026-01-02"},
		"updated_at":    {"2026-01-02;after", "2026-01-02"},
		"completed_at":  {"2026-01-02;after", "2026-01-02"},
		"start_date":    {"2026-01-02;after", "2026-01-02"},
		"target_date":   {"2026-01-02;after", "2026-01-02"},
		"type":          {"all", "active", "backlog"},
		"project":       {"00000000-0000-0000-0000-000000000001"},
		"cycle":         {"00000000-0000-0000-0000-000000000001", "None"},
		"module":        {"00000000-0000-0000-0000-000000000001", "None"},
		"intake_status": {"1"}, "inbox_status": {"1"},
		"sub_issue":         {"false", "true"},
		"subscriber":        {"00000000-0000-0000-0000-000000000001"},
		"start_target_date": {"true", "false"},
	}
	for name, candidates := range values {
		for _, candidate := range candidates {
			for _, method := range []string{"GET", "POST"} {
				filters := Parse(map[string]string{name: candidate}, method, "", time.Now())
				for lookup := range filters {
					mapped, known := issueFilterSQLMap[lookup]
					if !known {
						t.Errorf("%s=%s (%s) produces %q, which has no translation", name, candidate, method, lookup)
						continue
					}
					if mapped.family == unsupportedFilterFamily {
						t.Errorf("%s=%s (%s) produces %q, which is marked unsupported", name, candidate, method, lookup)
					}
				}
			}
		}
	}
}
