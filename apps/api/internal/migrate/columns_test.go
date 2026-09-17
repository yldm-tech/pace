package migrate

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestEveryColumnWrittenByHandExists is the guard against writing to a column that is not there.
//
// A table's columns move. db.0065 took is_onboarded, is_tour_completed, the theme and the billing address off users and put them on profiles, and two inserts elsewhere in this repository went on naming them — so god-mode's first admin could never be created, and the workspace seed's bot user could never be made. Both failed with "column does not exist", and both were only found by running them.
//
// Every literal column name handed to a Table(...).Create map is checked against the schema recorded from a fully migrated database. It is a blunt check and that is the point: it reads the names out of the source rather than being told what to look for, so a table renamed tomorrow is caught without anybody remembering to add it here.
func TestEveryColumnWrittenByHandExists(t *testing.T) {
	schema := columnsByTable(t)
	if len(schema) < 50 {
		t.Fatalf("only %d tables found in the recorded schema", len(schema))
	}
	for _, write := range handWrittenInserts(t) {
		columns, known := schema[write.table]
		if !known {
			t.Errorf("%s:%d writes to %q, which is not a table in the recorded schema", write.file, write.line, write.table)
			continue
		}
		for _, column := range write.columns {
			if !columns[column] {
				t.Errorf("%s:%d writes %s.%s, which is not a column in the recorded schema", write.file, write.line, write.table, column)
			}
		}
	}
}

// TestEveryInsertFillsTheRequiredColumns is the other half of the same guard.
//
// Django fills a column a caller does not mention from the field's default, and for a text field that allows empty strings with no explicit default that means "" rather than null. An insert written from reading the Python leaves such a column out and fails on a NOT NULL constraint the first time it runs — which is what happened to instances.domain, to profiles.company_name and to users.avatar, each found separately by running the thing.
//
// Audit columns are exempt: created_by_id and updated_by_id are nullable everywhere, and a row created by the system has nobody to attribute it to.
func TestEveryInsertFillsTheRequiredColumns(t *testing.T) {
	required := requiredByTable(t)
	for _, write := range handWrittenInserts(t) {
		columns, known := required[write.table]
		if !known {
			continue
		}
		supplied := map[string]bool{}
		for _, column := range write.columns {
			supplied[column] = true
		}
		for column := range columns {
			if !supplied[column] {
				t.Errorf("%s:%d writes to %s and does not supply %s, which is NOT NULL with no database default", write.file, write.line, write.table, column)
			}
		}
	}
}

// requiredByTable is every column that is NOT NULL and has no database default, which is every column an insert has to name.
func requiredByTable(t *testing.T) map[string]map[string]bool {
	t.Helper()
	contents, err := os.ReadFile("testdata/schema.tsv")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]map[string]bool{}
	for _, line := range strings.Split(string(contents), "\n") {
		kind, detail, found := strings.Cut(line, "\t")
		if !found || kind != "column" {
			continue
		}
		qualified, rest, found := strings.Cut(detail, " ")
		if !found || !strings.Contains(rest, "null=NO") || strings.Contains(rest, "default=") {
			continue
		}
		table, column, found := strings.Cut(qualified, ".")
		if !found {
			continue
		}
		if required[table] == nil {
			required[table] = map[string]bool{}
		}
		required[table][column] = true
	}
	return required
}

func columnsByTable(t *testing.T) map[string]map[string]bool {
	t.Helper()
	contents, err := os.ReadFile("testdata/schema.tsv")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]map[string]bool{}
	for _, line := range strings.Split(string(contents), "\n") {
		kind, detail, found := strings.Cut(line, "\t")
		if !found || kind != "column" {
			continue
		}
		qualified, _, found := strings.Cut(detail, " ")
		if !found {
			continue
		}
		table, column, found := strings.Cut(qualified, ".")
		if !found {
			continue
		}
		if schema[table] == nil {
			schema[table] = map[string]bool{}
		}
		schema[table][column] = true
	}
	return schema
}

type handWrittenInsert struct {
	file    string
	line    int
	table   string
	columns []string
}

var (
	createCall = regexp.MustCompile(`Table\("([a-z_]+)"\)\.Create\(map\[string\]any\{`)
	quotedKey  = regexp.MustCompile(`"([a-z_]+)":`)
)

// handWrittenInserts finds every Table("x").Create(map[string]any{...}) in the package tree and returns the keys it writes.
//
// The map's literal keys are what it reads, so a key built at runtime is invisible to it. Nothing here builds one, and a check that sees most of the problem beats no check at all.
func handWrittenInserts(t *testing.T) []handWrittenInsert {
	t.Helper()
	var found []handWrittenInsert
	root := ".."
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := os.ReadDir(root + "/" + entry.Name())
		if err != nil {
			continue
		}
		for _, file := range files {
			name := file.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := root + "/" + entry.Name() + "/" + name
			contents, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			found = append(found, insertsIn(entry.Name()+"/"+name, string(contents))...)
		}
	}
	if len(found) < 3 {
		t.Fatalf("only %d inserts found, so this guard is not reading the source", len(found))
	}
	return found
}

func insertsIn(file, source string) []handWrittenInsert {
	var found []handWrittenInsert
	lines := strings.Split(source, "\n")
	for index, line := range lines {
		match := createCall.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		write := handWrittenInsert{file: file, line: index + 1, table: match[1]}
		// The keys run from here to the closing brace of the literal, which is the first line whose only content is }).
		for _, rest := range lines[index:] {
			for _, key := range quotedKey.FindAllStringSubmatch(rest, -1) {
				write.columns = append(write.columns, key[1])
			}
			if strings.HasPrefix(strings.TrimSpace(rest), "}).") {
				break
			}
		}
		found = append(found, write)
	}
	return found
}

// TestEveryStructCreatedFillsTheRequiredColumns is the struct-shaped half.
//
// GORM writes the columns a struct has fields for and no others, so a NOT NULL column with no field is a null — which is how a new project came to fail on states.description, after the same class of bug had already been fixed three times in map form.
//
// Only structs that are actually handed to Create are checked. A struct used to read rows names whatever subset the query wants and is none of this test's business.
func TestEveryStructCreatedFillsTheRequiredColumns(t *testing.T) {
	required := requiredByTable(t)
	for _, model := range createdModels(t) {
		columns, known := required[model.table]
		if !known {
			continue
		}
		for column := range columns {
			if !model.columns[column] {
				t.Errorf("%s: %s is created from a struct with no field for %s, which is NOT NULL with no database default", model.file, model.name, column)
			}
		}
	}
}

type createdModel struct {
	file    string
	name    string
	table   string
	columns map[string]bool
	// kinds is the Go type behind each column, which is what says whether an unassigned field would be written as null.
	kinds map[string]string
}

var (
	structHead = regexp.MustCompile(`^type (\w+) struct \{$`)
	gormColumn = regexp.MustCompile("`gorm:\"[^\"]*column:([a-z_]+)")
	tableName  = regexp.MustCompile(`func \((\w+)\) TableName\(\) string \{ return "([a-z_]+)" \}`)
	createdBy  = regexp.MustCompile(`\.Create\(&(\w+)\)`)
)

// createdModels finds every struct with a TableName that something in the same package passes to Create.
func createdModels(t *testing.T) []createdModel {
	t.Helper()
	var found []createdModel
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := "../" + entry.Name()
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var sources []string
		var names []string
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
				continue
			}
			contents, err := os.ReadFile(dir + "/" + file.Name())
			if err != nil {
				continue
			}
			sources = append(sources, string(contents))
			names = append(names, entry.Name()+"/"+file.Name())
		}
		joined := strings.Join(sources, "\n")
		// The variables handed to Create, and the types they were built from.
		createdTypes := map[string]bool{}
		for _, match := range createdBy.FindAllStringSubmatch(joined, -1) {
			variable := match[1]
			// Both ways a slice or a value of the type is built: a literal, and make.
			for _, shape := range []string{` :?= (?:\[\])?(\w+)\{`, ` :?= make\(\[\](\w+)`} {
				for _, assignment := range regexp.MustCompile(regexp.QuoteMeta(variable)+shape).FindAllStringSubmatch(joined, -1) {
					createdTypes[assignment[1]] = true
				}
			}
			createdTypes[strings.Title(variable)] = true
		}
		for index, source := range sources {
			for _, table := range tableName.FindAllStringSubmatch(source, -1) {
				if !createdTypes[table[1]] {
					continue
				}
				columns, kinds := structColumns(source, table[1])
				if columns == nil {
					continue
				}
				found = append(found, createdModel{file: names[index], name: table[1], table: table[2], columns: columns, kinds: kinds})
			}
		}
	}
	return found
}

func structColumns(source, name string) (map[string]bool, map[string]string) {
	lines := strings.Split(source, "\n")
	for index, line := range lines {
		head := structHead.FindStringSubmatch(line)
		if head == nil || head[1] != name {
			continue
		}
		columns := map[string]bool{}
		kinds := map[string]string{}
		for _, field := range lines[index+1:] {
			if strings.TrimSpace(field) == "}" {
				break
			}
			match := gormColumn.FindStringSubmatch(field)
			if match == nil {
				continue
			}
			columns[match[1]] = true
			// The declared type sits between the field name and the tag.
			parts := strings.Fields(strings.TrimSpace(field))
			if len(parts) >= 2 {
				kinds[match[1]] = parts[1]
			}
		}
		return columns, kinds
	}
	return nil, nil
}
