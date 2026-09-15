package project

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// fullUpdateRequirements is what DRF's partial=False adds on each PUT route: the serializer's required fields must all be present.
//
// The sets came from asking each serializer which of its fields are required and writable, rather than from reading the models. A comment requires nothing, which is why PUT and PATCH behave alike there.
var fullUpdateRequirements = map[string][]string{
	"project": {"identifier", "name"},
	"cycle":   {"name"},
	"issue":   {"name"},
	"comment": {},
	"link":    {"url"},
}

// requireFullUpdateFields is the difference between PUT and PATCH. Everything else about the two is the same, so each PUT route runs the partial handler once this has passed.
func (handler *Handler) requireFullUpdateFields(c *gin.Context, kind string) bool {
	body, err := readBodyForReplay(c)
	if err != nil {
		handler.invalidDetail(c)
		return false
	}
	missing := gin.H{}
	for _, field := range fullUpdateRequirements[kind] {
		raw, present := body[field]
		if !present || string(raw) == "null" {
			missing[field] = []string{"This field is required."}
		}
	}
	if len(missing) > 0 {
		c.JSON(http.StatusBadRequest, missing)
		return false
	}
	return true
}

// fullUpdate wraps a partial handler so the same code serves both methods.
func (handler *Handler) fullUpdate(kind string, partial func(*gin.Context, *auth.User)) func(*gin.Context, *auth.User) {
	return func(c *gin.Context, user *auth.User) {
		if !handler.requireFullUpdateFields(c, kind) {
			return
		}
		partial(c, user)
	}
}

// readBodyForReplay reads the request body and puts it back, so the handler that runs next can read it too.
func readBodyForReplay(c *gin.Context) (map[string]json.RawMessage, error) {
	var body map[string]json.RawMessage
	if err := c.ShouldBindBodyWithJSON(&body); err != nil {
		return nil, err
	}
	return body, nil
}
