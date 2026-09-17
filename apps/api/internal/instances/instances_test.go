package instances

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testHandler(settings Settings) *Handler {
	return &Handler{settings: settings}
}

// The console's own origin is where every redirect from here goes, with its base path on the end. Without an origin of its own it is the web origin with that path.
func TestTheAdminConsoleOrigin(t *testing.T) {
	cases := []struct {
		settings Settings
		want     string
	}{
		{Settings{AdminBaseURL: "https://admin.test", AdminBasePath: "/admin/"}, "https://admin.test/admin/"},
		// A path with no slashes on either end gets both.
		{Settings{AdminBaseURL: "https://admin.test", AdminBasePath: "console"}, "https://admin.test/console/"},
		// No path at all falls back to admin.
		{Settings{AdminBaseURL: "https://admin.test"}, "https://admin.test/admin/"},
		// No origin of its own means the web origin.
		{Settings{WebURL: "https://plane.test", AdminBasePath: "/admin/"}, "https://plane.test/admin/"},
		// And failing that the app origin.
		{Settings{AppBaseURL: "https://app.test", AdminBasePath: "/admin/"}, "https://app.test/admin/"},
	}
	for _, testCase := range cases {
		if got := testHandler(testCase.settings).adminBaseURL(); got != testCase.want {
			t.Errorf("%#v gives %q rather than %q", testCase.settings, got, testCase.want)
		}
	}
}

// A refusal redirects back to the console with the reason in the query string, which is the only way the console learns what went wrong.
func TestARefusalRedirectsWithItsReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := testHandler(Settings{AdminBaseURL: "https://admin.test", AdminBasePath: "/admin/"})
	router := gin.New()
	router.POST("/refuse", func(c *gin.Context) {
		handler.redirectError(c, handler.adminBaseURL(), errorAdminUserDoesNotExist, "ADMIN_USER_DOES_NOT_EXIST",
			map[string]string{"email": "ada@example.test"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/refuse", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("the status is %d", recorder.Code)
	}
	location := recorder.Header().Get("Location")
	for _, part := range []string{
		"https://admin.test/admin/?",
		"error_code=5185",
		"error_message=ADMIN_USER_DOES_NOT_EXIST",
		"email=ada%40example.test",
	} {
		if !strings.Contains(location, part) {
			t.Errorf("the redirect %q is missing %q", location, part)
		}
	}
}

// The telemetry answer is stored as a boolean from whatever the form sent, and any non-empty answer is true — so "false" leaves telemetry on.
func TestTheTelemetryAnswer(t *testing.T) {
	if !telemetryEnabled("True") {
		t.Error("True turned telemetry off")
	}
	if !telemetryEnabled("false") {
		t.Error(`"false" turned telemetry off, and upstream leaves it on`)
	}
	if telemetryEnabled("") {
		t.Error("an empty answer left telemetry on")
	}
}

// A configuration value is trimmed and turned into text on its way into the column, and a null empties it.
func TestTheConfigurationValueOnItsWayIn(t *testing.T) {
	current := "kept"
	cases := []struct {
		value any
		want  string
	}{
		{"  spaced  ", "spaced"},
		{"", ""},
		{nil, ""},
		{float64(587), "587"},
		{true, "True"},
		{false, "False"},
	}
	for _, testCase := range cases {
		if got := configurationText(testCase.value, &current); got != testCase.want {
			t.Errorf("%#v becomes %q rather than %q", testCase.value, got, testCase.want)
		}
	}
}

// The thirteen keys the config response is built from, with the fallbacks the view declares — three of which disagree with what configure_instance seeds.
func TestTheConfigurationFallbacks(t *testing.T) {
	if len(instanceConfigurationKeys) != 13 {
		t.Fatalf("there are %d keys", len(instanceConfigurationKeys))
	}
	fallbacks := map[string]string{}
	for _, entry := range instanceConfigurationKeys {
		fallbacks[entry.Key] = entry.Fallback
	}
	// Signup falls back to off here and is seeded on; the magic link falls back to on here and is seeded off.
	if fallbacks["ENABLE_SIGNUP"] != "0" {
		t.Errorf("signup falls back to %q", fallbacks["ENABLE_SIGNUP"])
	}
	if fallbacks["ENABLE_MAGIC_LINK_LOGIN"] != "1" {
		t.Errorf("the magic link falls back to %q", fallbacks["ENABLE_MAGIC_LINK_LOGIN"])
	}
	if fallbacks["ENABLE_EMAIL_PASSWORD"] != "1" {
		t.Errorf("the email password falls back to %q", fallbacks["ENABLE_EMAIL_PASSWORD"])
	}
}

// The email the admin console signs in with is validated before anything is looked up, which is what keeps a malformed one out of the query.
func TestTheSignInEmailCheck(t *testing.T) {
	for _, valid := range []string{"ada@example.test", "a.b+c@sub.example.test"} {
		if !validEmail(valid) {
			t.Errorf("%q was refused", valid)
		}
	}
	for _, invalid := range []string{"", "ada", "ada@", "@example.test", "ada@example", "a b@example.test", "a@b@example.test"} {
		if validEmail(invalid) {
			t.Errorf("%q was accepted", invalid)
		}
	}
}

// Only the fields the serializer leaves writable may be patched onto the registration, and the four it names read-only are ignored rather than refused.
func TestTheWritableInstanceFields(t *testing.T) {
	for _, writable := range []string{"instance_name", "is_telemetry_enabled", "domain"} {
		if _, ok := instanceWritableFields[writable]; !ok {
			t.Errorf("%s is not writable and the serializer leaves it so", writable)
		}
	}
	// read_only_fields names id, email, last_checked_at and is_setup_done — and email is not a field the model has at all.
	for _, readOnly := range []string{"id", "last_checked_at", "is_setup_done", "created_at", "email"} {
		if _, ok := instanceWritableFields[readOnly]; ok {
			t.Errorf("%s is writable and it should not be", readOnly)
		}
	}
}

// The six mail settings the disable route clears, which is the switch plus the five it empties.
func TestTheEmailKeysTheDisableRouteClears(t *testing.T) {
	want := map[string]bool{
		"EMAIL_HOST": true, "EMAIL_HOST_USER": true, "EMAIL_HOST_PASSWORD": true,
		"ENABLE_SMTP": true, "EMAIL_PORT": true, "EMAIL_FROM": true,
	}
	if len(emailConfigurationKeys) != len(want) {
		t.Fatalf("there are %d keys", len(emailConfigurationKeys))
	}
	for _, key := range emailConfigurationKeys {
		if !want[key] {
			t.Errorf("%s is not one of the mail settings", key)
		}
	}
}
