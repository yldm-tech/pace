package project

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api-go/internal/auth"
)

// genericActionMisconfigured answers the three methods on the two work item detail paths that cannot work.
//
// `cycle-issues/<issue_id>/` and `modules/<module_id>/issues/<issue_id>/` each bind four methods. The delete is written by hand and works. The other three — `GET`, `PUT` and `PATCH` — are DRF's own generic actions, and a generic action finds its row through `get_object()`, which looks for a URL keyword called `pk`. Neither path has one: they are named `issue_id`. So `get_object` fails its own assertion before touching the database, the base view turns anything it does not recognise into one sentence and a 500, and that is what the caller gets.
//
// It is the same answer for a work item that exists, one that does not, and one in another project, because nothing is ever looked up. Reproduced rather than corrected: cutting these paths over means owning every method on them, and a 404 or a working read here would be a different answer to the same request.
func (handler *Handler) genericActionMisconfigured(c *gin.Context, _ *auth.User) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Something went wrong please try again later"})
}
