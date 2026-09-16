package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoDjangoOrderingReachesSQL covers both shapes of the same mistake.
//
// sanitizeOrderBy speaks Django's ordering syntax, where a leading minus means descending. Concatenated after a table alias that becomes `p.-created_at`, which Postgres rejects with `syntax error at or near "-"`, and the endpoint answers 500 for every request that does not name an order -- which is what a client sends by default.
//
// #201 fixed two of these and guarded the shape it had seen: Order("v." + sanitizeOrderBy(...)) written inline. It missed the other shape, where the result is assigned first and the variable is concatenated a few lines later, and projects-lite on the external API stayed broken. This walks both.
func TestNoDjangoOrderingReachesSQL(t *testing.T) {
	inline := regexp.MustCompile(`Order\([^)]*"\s*\+\s*sanitizeOrderBy\(`)
	assigned := regexp.MustCompile(`(\w+)\s*:?=\s*sanitizeOrderBy\(`)

	var offenders []string
	root := filepath.Join("..", "..")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(contents)
		lines := strings.Split(text, "\n")

		names := map[string]bool{}
		for _, match := range assigned.FindAllStringSubmatch(text, -1) {
			names[match[1]] = true
		}

		for index, line := range lines {
			if inline.MatchString(line) {
				offenders = append(offenders, path+":"+strings.TrimSpace(line))
				continue
			}
			for name := range names {
				// Order("p." + order) or Order(order): the value goes in without being turned into a direction.
				concatenated := regexp.MustCompile(`Order\([^)]*"\s*\+\s*` + regexp.QuoteMeta(name) + `\b`)
				bare := regexp.MustCompile(`Order\(\s*` + regexp.QuoteMeta(name) + `\s*\)`)
				if concatenated.MatchString(line) || bare.MatchString(line) {
					offenders = append(offenders, path+":"+strings.TrimSpace(line))
				}
			}
			_ = index
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("sanitizeOrderBy returns Django's syntax; pass it through orderClause before it reaches SQL:\n  %s", strings.Join(offenders, "\n  "))
	}
}
