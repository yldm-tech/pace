package drf

import (
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
)

var timeType = reflect.TypeOf(time.Time{})

// Respond writes a success body, rewriting every datetime in it to DRF's ISO-8601 rendering on the way out.
//
// The rewrite happens here rather than at each field because a serializer that forgets it produces a body that is wrong in about a tenth of its timestamps and identical the rest of the time — a difference no ordinary test notices. Routing every success response through one function makes forgetting impossible, and the guard test in this package fails the build if a new route writes one directly.
//
// Error bodies still go through c.JSON: they carry no datetimes, and keeping them on the plain path keeps this function's contract narrow.
func Respond(c *gin.Context, status int, payload any) {
	c.JSON(status, convert(payload))
}

// convert walks maps and slices, replacing datetimes. It copies rather than mutating in place, so a caller that keeps serializing the same map — the sub-issue grouping files one issue under several assignees — is not left holding rewritten values.
func convert(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case time.Time:
		return Time(typed)
	case *time.Time:
		return At(typed)
	case Time, *Time:
		return value
	case gin.H:
		return convertMap(typed)
	case map[string]any:
		return convertMap(typed)
	case []gin.H:
		converted := make([]any, len(typed))
		for index, item := range typed {
			converted[index] = convertMap(item)
		}
		return converted
	case []any:
		converted := make([]any, len(typed))
		for index, item := range typed {
			converted[index] = convert(item)
		}
		return converted
	}
	return convertReflected(value)
}

func convertMap(value map[string]any) map[string]any {
	converted := make(map[string]any, len(value))
	for key, item := range value {
		converted[key] = convert(item)
	}
	return converted
}

// convertReflected catches the shapes the type switch cannot name: a slice of some other element type, or a pointer to one. Anything that is not a container, and every struct, is handed back untouched — a struct field is the serializers' business, and reaching into one would rewrite values the caller deliberately typed.
func convertReflected(value any) any {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array:
		if reflected.Kind() == reflect.Slice && reflected.IsNil() {
			return value
		}
		// A byte slice is data, not a list of values to walk.
		if reflected.Type().Elem().Kind() == reflect.Uint8 {
			return value
		}
		converted := make([]any, reflected.Len())
		for index := range converted {
			converted[index] = convert(reflected.Index(index).Interface())
		}
		return converted
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return value
		}
		converted := make(map[string]any, reflected.Len())
		iterator := reflected.MapRange()
		for iterator.Next() {
			converted[iterator.Key().String()] = convert(iterator.Value().Interface())
		}
		return converted
	case reflect.Pointer:
		if reflected.IsNil() {
			return value
		}
		if reflected.Type().Elem() == timeType {
			return Time(reflected.Elem().Interface().(time.Time))
		}
	}
	return value
}
