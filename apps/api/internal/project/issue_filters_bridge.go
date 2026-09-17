package project

import (
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/issuefilters"
)

// The filter machinery lives in internal/issuefilters, because the space app reads the same parameters off its own query strings. These names keep the call sites in this package reading the way they did.

type filterValue = issuefilters.Value

func issueFilters(params map[string]string, method, prefix string, today time.Time) map[string]filterValue {
	return issuefilters.Parse(params, method, prefix, today)
}

func issueFilterSQL(filters map[string]filterValue) ([]string, []string, []any, bool) {
	return issuefilters.SQL(filters)
}

func listValue(values []string) filterValue { return issuefilters.ListValue(values) }
func textValue(value string) filterValue    { return issuefilters.TextValue(value) }
func boolValue(value bool) filterValue      { return issuefilters.BoolValue(value) }

func containsString(items []string, needle string) bool {
	return issuefilters.ContainsString(items, needle)
}

var issueFilterOrder = issuefilters.Order

var relativeTermPattern = issuefilters.RelativeTermPattern

func splitToCharacters(value string) []string { return issuefilters.SplitToCharacters(value) }

func applyDateQueries(result map[string]filterValue, term string, queries []string, today time.Time) {
	issuefilters.ApplyDateQueries(result, term, queries, today)
}
