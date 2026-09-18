package workspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yldm-tech/pace/apps/api/internal/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// requireProjectMember is the whole gate on the project asset routes, and its query is the only thing in them that reads the caller's membership. It is not a pure function and this package's database-backed tests are skipped unless WORKSPACE_TEST_DATABASE_URL is set, so what this does is run the gate against a session that records the statement instead of sending it, and read the predicate back out of the record.

// recordedStatements is a gorm logger that keeps the SQL a session would have sent. Under DryRun nothing is sent, and the trace still carries the statement with its bind values inlined.
type recordedStatements struct {
	statements []string
}

func (recorded *recordedStatements) LogMode(logger.LogLevel) logger.Interface { return recorded }

func (recorded *recordedStatements) Info(context.Context, string, ...any) {}

func (recorded *recordedStatements) Warn(context.Context, string, ...any) {}

func (recorded *recordedStatements) Error(context.Context, string, ...any) {}

func (recorded *recordedStatements) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	recorded.statements = append(recorded.statements, statement)
}

func (recorded *recordedStatements) contains(fragment string) bool {
	for _, statement := range recorded.statements {
		if strings.Contains(statement, fragment) {
			return true
		}
	}
	return false
}

func (recorded *recordedStatements) String() string {
	return strings.Join(recorded.statements, "\n  ")
}

// recordingHandler is a Handler whose queries are built and recorded rather than run. DisableAutomaticPing is what keeps gorm.Open from dialling the DSN, which is never connected to.
func recordingHandler(t *testing.T) (*Handler, *recordedStatements) {
	t.Helper()
	recorded := &recordedStatements{}
	database, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "postgres://render@127.0.0.1:1/render"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: recorded},
	)
	if err != nil {
		t.Fatalf("opening a dry-run database: %v", err)
	}
	return &Handler{db: database}, recorded
}

// The gate asks project_members through its own manager and has no other liveness condition in it, so the membership's own soft delete is what stops a member of a soft-deleted project from reading and writing its assets. It arrives with the worker's cascade rather than with the delete, which is a lag and not a hole: the project row itself is stamped first, and no route behind this gate is reachable without the project.
func TestTheProjectAssetGateRefusesACascadedMembership(t *testing.T) {
	handler, recorded := recordingHandler(t)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	context.Params = gin.Params{
		{Key: "slug", Value: "acme"},
		{Key: "id", Value: "00000000-0000-0000-0000-0000000000p1"},
	}

	if handler.requireProjectMember(context, &auth.User{ID: "00000000-0000-0000-0000-0000000000u1"}) {
		t.Error("the gate admitted a caller whose membership lookup matched nothing")
	}
	if recorder.Code != http.StatusForbidden {
		t.Errorf("the gate answered %d, want %d", recorder.Code, http.StatusForbidden)
	}
	for _, fragment := range []string{"pm.is_active = TRUE", "pm.deleted_at IS NULL"} {
		if !recorded.contains(fragment) {
			t.Errorf("the gate does not filter %s:\n  %s", fragment, recorded)
		}
	}
}
