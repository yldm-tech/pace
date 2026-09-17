package live

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain quiets the router's own start-up logging, which otherwise prints every route for every server a test starts.
func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	os.Exit(m.Run())
}
