package drf

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The corpus is every rendering Python produced, keyed by the exact bit pattern so nothing is lost on the way through a decimal string.
func TestFloatRenderingMatchesPython(t *testing.T) {
	file, err := os.Open("testdata/float_rendering.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		t.Fatal("the fixture has no header")
	}
	rows := 0
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 2 {
			t.Fatalf("row %d has %d columns, want 2", rows+1, len(columns))
		}
		bits, err := strconv.ParseUint(columns[0], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		value := math.Float64frombits(bits)
		if got := FormatFloat(value); got != columns[1] {
			t.Errorf("FormatFloat(%v) = %s, Python renders %s", value, got, columns[1])
		}
		encoded, err := json.Marshal(Float(value))
		if err != nil {
			t.Fatalf("marshalling %v: %v", value, err)
		}
		if string(encoded) != columns[1] {
			t.Errorf("json.Marshal(Float(%v)) = %s, Python renders %s", value, encoded, columns[1])
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows < 400 {
		t.Fatalf("the fixture has %d rows, which is too few to cover the exponent thresholds", rows)
	}
}

// Go's own encoder disagrees on the cases the type exists for, which is worth stating rather than trusting.
func TestGoAndPythonDisagreeOnTheseValues(t *testing.T) {
	for _, value := range []float64{0, 1, 65535, 1e16, 1e-5} {
		plain, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		wrapped, err := json.Marshal(Float(value))
		if err != nil {
			t.Fatal(err)
		}
		if string(plain) == string(wrapped) {
			t.Errorf("%v renders as %s either way, so the fixture is not covering what it claims", value, plain)
		}
	}
}

// A value Python would write as NaN or Infinity is not JSON. Refusing is louder than emitting a body no parser accepts.
func TestNonFiniteValuesAreRefused(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := json.Marshal(Float(value)); err == nil {
			t.Errorf("marshalling %v succeeded, want a refusal", value)
		}
	}
}
