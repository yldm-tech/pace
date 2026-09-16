package live

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/ydoc"
)

// convertRequest is what the route is asked with.
type convertRequest struct {
	DescriptionHTML string `json:"description_html"`
	Variant         string `json:"variant"`
}

// validationError is one complaint about the request, in the shape the caller reads.
type validationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// convertDocument turns HTML into the representations the editor reads.
//
// The API calls it when it copies a page or a work item description: the assets in the HTML have been swapped for their copies, and the Yjs document and the JSON beside it have to be rebuilt from the result. It is the one route here that is not about a live connection.
//
// Only two of the three representations go back. The HTML the caller sent is the HTML it keeps, even though converting it changes it — which is upstream's shape and is what keeps a copy's HTML identical to the original's.
func (s *Server) convertDocument(c *gin.Context) {
	var request convertRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil {
		s.logger.Warn("could not read a conversion request", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"message": "Validation error", "context": gin.H{"validationErrors": []validationError{}}})
		return
	}

	if problems := validateConvertRequest(request); len(problems) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Validation error", "context": gin.H{"validationErrors": problems}})
		return
	}

	variant, err := ydoc.VariantFor(request.Variant)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Validation error", "context": gin.H{"validationErrors": []validationError{{Path: "variant", Message: "Invalid enum value. Expected 'rich' | 'document', received '" + request.Variant + "'"}}}})
		return
	}

	formats, err := ydoc.ConvertHTML(request.DescriptionHTML, variant)
	if err != nil {
		s.logger.Error("could not convert a document", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Internal server error."})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"description_json":   formats.DescriptionJSON,
		"description_binary": formats.DescriptionBinary,
	})
}

// validateConvertRequest is the same set of checks the request is put through upstream, in the same order, so that a caller reading the complaints sees the same ones.
func validateConvertRequest(request convertRequest) []validationError {
	var problems []validationError

	switch {
	case request.DescriptionHTML == "":
		problems = append(problems, validationError{Path: "description_html", Message: "HTML content cannot be empty"})
	case strings.TrimSpace(request.DescriptionHTML) == "":
		problems = append(problems, validationError{Path: "description_html", Message: "HTML content cannot be just whitespace"})
	case !strings.Contains(request.DescriptionHTML, "<") || !strings.Contains(request.DescriptionHTML, ">"):
		problems = append(problems, validationError{Path: "description_html", Message: "Content must be valid HTML"})
	}

	if request.Variant != "rich" && request.Variant != "document" {
		problems = append(problems, validationError{
			Path:    "variant",
			Message: "Invalid enum value. Expected 'rich' | 'document', received '" + request.Variant + "'",
		})
	}
	return problems
}
