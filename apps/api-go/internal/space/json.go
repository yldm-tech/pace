package space

import "github.com/yldm-tech/pace/apps/api-go/internal/drf"

// decodeJSON reads a jsonb column into the shape a response should carry, keeping a blob's own numbers.
func decodeJSON(value []byte) any {
	return drf.DecodeJSON(value)
}
