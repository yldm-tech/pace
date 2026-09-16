package manage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every configuration variable the Django app seeds is in the embedded list, with the environment variable it reads and whether it is stored encrypted.
func TestTheInstanceConfigVariables(t *testing.T) {
	variables := instanceConfigVariables()
	if len(variables) < 30 {
		t.Fatalf("only %d configuration variables are embedded", len(variables))
	}
	byKey := map[string]configVariable{}
	for _, variable := range variables {
		if variable.Key == "" || variable.Category == "" {
			t.Errorf("a variable is missing its key or category: %#v", variable)
		}
		if _, seen := byKey[variable.Key]; seen {
			t.Errorf("%s is listed twice", variable.Key)
		}
		byKey[variable.Key] = variable
	}

	// The three that decide whether anybody can sign in at all.
	for key, want := range map[string]string{
		"ENABLE_SIGNUP": "1", "ENABLE_EMAIL_PASSWORD": "1", "ENABLE_MAGIC_LINK_LOGIN": "0",
	} {
		variable, known := byKey[key]
		if !known {
			t.Fatalf("%s is not in the list", key)
		}
		if variable.Default != want {
			t.Errorf("%s defaults to %q rather than %q", key, variable.Default, want)
		}
		if variable.Encrypted {
			t.Errorf("%s is stored encrypted, and it is not a secret", key)
		}
	}

	// At least one secret is stored encrypted, which is what the encryption path exists for.
	encrypted := 0
	for _, variable := range variables {
		if variable.Encrypted {
			encrypted++
		}
	}
	if encrypted == 0 {
		t.Error("no configuration variable is stored encrypted")
	}
}

// The instance identifier is twenty-four hex characters, which is what secrets.token_hex(12) gives.
func TestTheInstanceIdentifier(t *testing.T) {
	first, err := instanceIdentifier()
	if err != nil {
		t.Fatalf("building an identifier: %v", err)
	}
	if len(first) != 24 {
		t.Errorf("the identifier is %d characters: %q", len(first), first)
	}
	second, _ := instanceIdentifier()
	if first == second {
		t.Error("two identifiers came out the same")
	}
}

// TestTheInstanceRowFillsEveryRequiredColumn is the guard against a class of bug, not one instance of it.
//
// Django fills a column a command does not mention from the field's default, and for a text field that allows empty strings with no explicit default that means the empty string rather than null. A Go insert written from reading the Python leaves such a column out, and the first time it runs it fails on a NOT NULL constraint — which is what register_instance did with domain.
//
// The schema fixture internal/migrate keeps is the authoritative list of which columns those are, so it is read here rather than restated.
func TestTheInstanceRowFillsEveryRequiredColumn(t *testing.T) {
	required := requiredColumnsOf(t, "instances")
	if len(required) < 10 {
		t.Fatalf("only %d required columns found for instances, so this guard is not reading the schema", len(required))
	}
	row := newInstanceRow("identifier", "v0.0.0", "v0.0.0", time.Now().UTC(), false)
	for _, column := range required {
		if _, supplied := row[column]; !supplied {
			t.Errorf("instances.%s is NOT NULL with no database default and the insert does not supply it", column)
		}
	}
}

// requiredColumnsOf reads the columns of one table that are NOT NULL and have no database default, out of the schema recorded from a Django-migrated database.
func requiredColumnsOf(t *testing.T, table string) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "migrate", "testdata", "schema.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for _, line := range strings.Split(string(contents), "\n") {
		kind, detail, found := strings.Cut(line, "\t")
		if !found || kind != "column" {
			continue
		}
		name, rest, found := strings.Cut(detail, " ")
		if !found || !strings.HasPrefix(name, table+".") {
			continue
		}
		if !strings.Contains(rest, "null=NO") || strings.Contains(rest, "default=") {
			continue
		}
		columns = append(columns, strings.TrimPrefix(name, table+"."))
	}
	return columns
}
