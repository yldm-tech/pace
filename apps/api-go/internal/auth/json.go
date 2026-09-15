package auth

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONValue keeps PostgreSQL json/jsonb parameters textual. A plain []byte is
// encoded by pgx as bytea, which PostgreSQL cannot assign to a jsonb column.
type JSONValue []byte

func (value JSONValue) Value() (driver.Value, error) {
	if len(value) == 0 {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("invalid JSON value")
	}
	return string(value), nil
}

func (value *JSONValue) Scan(source any) error {
	if source == nil {
		*value = nil
		return nil
	}
	var encoded []byte
	switch source := source.(type) {
	case []byte:
		encoded = source
	case string:
		encoded = []byte(source)
	default:
		return fmt.Errorf("scan JSON value from %T", source)
	}
	if !json.Valid(encoded) {
		return fmt.Errorf("scan invalid JSON value")
	}
	*value = append((*value)[:0], encoded...)
	return nil
}
