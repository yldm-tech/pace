package externalapi

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

// The external API's permission gates are the whole of its authorization: the API-key middleware in auth.go authenticates the key and the user and says nothing about the workspace or the project, so whatever the gate's query does not filter, the route does not filter either. There is no database-backed test in this package to check them against, and these gates are not pure functions, so what these tests do is run the gate against a session that records the statement instead of sending it, and read the predicate back out of the record.
//
// That gives two assertions per gate, and both matter. The recorded SQL says which rows the gate would have accepted. The gate's own answer, against a session where no query matches anything, says which way it fails when it finds nothing — and the answer has to be no.

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

// contains reports whether any recorded statement carries the fragment.
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

// gateRequest is a request carrying the two route parameters every project gate reads.
func gateRequest(t *testing.T, method string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, "/", nil)
	context.Params = gin.Params{
		{Key: "slug", Value: "acme"},
		{Key: "project", Value: "00000000-0000-0000-0000-0000000000p1"},
	}
	return context, recorder
}

// gateUser is the caller every gate below is asked about.
func gateUser() *auth.User {
	return &auth.User{ID: "00000000-0000-0000-0000-0000000000u1"}
}

// ProjectBasePermission reads the caller's workspace role, and a soft-deleted workspace has no members to read: the workspace's own deleted_at is written in the same transaction as the delete, so this is the filter that closes the window rather than narrowing it eventually.
//
// This gate used to filter neither the workspace's soft delete nor the membership's, alone among the five copies of the lookup in the tree, and it is the one that admits the external API's project list, create, update and delete.
func TestTheProjectBaseGateWillNotReadASoftDeletedWorkspace(t *testing.T) {
	handler, recorded := recordingHandler(t)
	context, recorder := gateRequest(t, http.MethodGet)

	if handler.requireProjectBase(context, gateUser(), http.MethodGet) {
		t.Error("the gate admitted a caller whose membership lookup matched nothing")
	}
	if recorder.Code != http.StatusForbidden {
		t.Errorf("the gate answered %d, want %d", recorder.Code, http.StatusForbidden)
	}
	for _, fragment := range []string{
		"w.deleted_at IS NULL",
		"wm.is_active = TRUE",
		"wm.deleted_at IS NULL",
	} {
		if !recorded.contains(fragment) {
			t.Errorf("the workspace role lookup does not filter %s:\n  %s", fragment, recorded)
		}
	}
}
