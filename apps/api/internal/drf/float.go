package drf

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
)

// Float is a number rendered the way a Python JSON body renders one.
//
// Go's encoding/json writes a float with the shortest digits that round-trip and a plain decimal point, so 65535.0 comes out as `65535` — indistinguishable from an integer. Python's json.dumps hands a float to repr(), which always leaves a decimal point behind: the same value is `65535.0`. Every sort_order, every estimate sum and every progress total is a Django FloatField, so the difference is on most responses rather than a few.
//
// The two also disagree about when to switch to exponential notation. encoding/json switches below 1e-6 and at or above 1e21; Python switches when the decimal point would sit past the sixteenth digit or more than four places left of the first one. Between those thresholds lie values like 1e16, which Go writes in full and Python writes as `1e+16`.
type Float float64

// FormatFloat is the rendering itself, without quotes. It is CPython's format_float_short in 'r' mode with ADD_DOT_0, which is what repr() and therefore json.dumps use.
func FormatFloat(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		// Python writes NaN, Infinity and -Infinity, which are not JSON. Nothing in the schema can hold one — every float here is a sum or a sort order — so rather than emit a body no parser accepts, MarshalJSON refuses and the request fails loudly.
		return ""
	}
	// The shortest round-tripping digits, from which the position of the decimal point falls out.
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	exponent, err := strconv.Atoi(scientific[indexOfExponent(scientific)+1:])
	if err != nil {
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
	// decpt is where the decimal point lands among the significant digits: 1 for 1.5, 17 for 1e16.
	decpt := exponent + 1
	if decpt <= -4 || decpt > 16 {
		return scientific
	}
	plain := strconv.FormatFloat(value, 'f', -1, 64)
	for index := 0; index < len(plain); index++ {
		if plain[index] == '.' {
			return plain
		}
	}
	// ADD_DOT_0: a rendering with no point left in it would read as an integer.
	return plain + ".0"
}

func indexOfExponent(value string) int {
	for index := len(value) - 1; index >= 0; index-- {
		if value[index] == 'e' {
			return index
		}
	}
	return len(value) - 1
}

func (value Float) MarshalJSON() ([]byte, error) {
	rendered := FormatFloat(float64(value))
	if rendered == "" {
		return nil, &json.UnsupportedValueError{Value: reflect.ValueOf(float64(value)), Str: strconv.FormatFloat(float64(value), 'g', -1, 64)}
	}
	return []byte(rendered), nil
}

// DecodeJSON reads a jsonb column into the shape a response should carry.
//
// It exists because Respond now rewrites every float it walks, and a number that came out of a jsonb blob must not be rewritten: psycopg hands Django the blob's own parse, so an integer stored in view_props comes back as an integer and a float comes back as a float. Go's plain decoder turns both into float64 and the distinction is gone before Respond ever sees it.
//
// json.Number keeps the literal text the column held, which renders back exactly as it was stored and is what Django does in effect.
func DecodeJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if decoder.Decode(&decoded) != nil {
		return nil
	}
	return decoded
}
