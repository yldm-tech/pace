package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// This is the first route to run stored editor HTML through the sanitizer
// ported in the htmlsanitizer package, so the behaviour is pinned here.
func TestCommentHTMLIsSanitizedOnTheWayIn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{name: "script is dropped with its content", in: `<p>keep<script>alert(1)</script></p>`, want: `<p>keep</p>`},
		{name: "event handler attribute is dropped", in: `<p onclick="x()">text</p>`, want: `<p>text</p>`},
		{name: "javascript href is dropped", in: `<a href="javascript:alert(1)">bad</a>`, want: `<a rel="noopener noreferrer">bad</a>`},
		{name: "unclosed markup is repaired", in: `<p>unclosed`, want: `<p>unclosed</p>`},
		{name: "allowed markup survives", in: `<p>Hello <strong>world</strong></p>`, want: `<p>Hello <strong>world</strong></p>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed := runCommentFields(t, map[string]any{"comment_html": test.in})
			if parsed.commentHTML != test.want {
				t.Fatalf("comment_html = %q, want %q", parsed.commentHTML, test.want)
			}
		})
	}
}

func TestCommentValidationMatchesDjango(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "null comment_html", body: `{"comment_html":null}`, want: `{"comment_html":["This field may not be null."]}`},
		{name: "non string comment_html", body: `{"comment_html":5}`, want: `{"comment_html":["Not a valid string."]}`},
		{name: "null comment_json", body: `{"comment_json":null}`, want: `{"comment_json":["This field may not be null."]}`},
		{name: "invalid access", body: `{"access":"PUBLIC"}`, want: `{"access":["\"PUBLIC\" is not a valid choice."]}`},
		{name: "bad parent", body: `{"parent":"nope"}`, want: `{"parent":["“nope” is not a valid UUID."]}`},
		{name: "long external source", body: `{"external_source":"` + strings.Repeat("s", 256) + `"}`, want: `{"external_source":["Ensure this field has no more than 255 characters."]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			handler := NewHandler(nil, nil, Settings{})
			router.POST("/comments", func(c *gin.Context) {
				var body map[string]json.RawMessage
				if err := c.ShouldBindJSON(&body); err != nil {
					handler.invalidDetail(c)
					return
				}
				_, _ = handler.issueCommentFieldsFrom(c, body)
			})
			request := httptest.NewRequest(http.MethodPost, "/comments", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.String() != test.want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

// IssueComment.save derives comment_stripped with Django's strip_tags, and
// leaves it empty when the html is empty rather than running the stripper.
func TestCommentStrippedMatchesTheModelSave(t *testing.T) {
	for _, test := range []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "<p>Hello <strong>world</strong></p>", want: "Hello world"},
		{in: "<p></p>", want: ""},
		{in: "plain", want: "plain"},
	} {
		if got := strippedComment(test.in); got != test.want {
			t.Errorf("strippedComment(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestCommentRoutesRejectMalformedIdentifiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(nil, nil, Settings{}).Register(router)
	project := "/api/workspaces/pace/projects/01234567-89ab-4def-8123-456789abcdef/"
	for _, path := range []string{
		project + "issues/11111111-2222-4333-8444-555555555555/comments/not-a-uuid/",
		project + "comments/not-a-uuid/reactions/",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}

func runCommentFields(t *testing.T, body map[string]any) issueCommentInput {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler := NewHandler(nil, nil, Settings{})
	var parsed issueCommentInput
	router.POST("/comments", func(c *gin.Context) {
		var raw map[string]json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			handler.invalidDetail(c)
			return
		}
		parsed, _ = handler.issueCommentFieldsFrom(c, raw)
	})
	request := httptest.NewRequest(http.MethodPost, "/comments", strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	return parsed
}

// TestInvalidChoiceQuotesTheValue covers the case the old spelling got wrong. It wrapped the value in literal quotes, so a value that contained one produced a message nobody could read back -- and a reader could not tell where the value ended. strconv.Quote escapes it instead.
func TestInvalidChoiceQuotesTheValue(t *testing.T) {
	for _, test := range []struct {
		value any
		want  string
	}{
		{"high", `"high" is not a valid choice.`},
		{nil, `"None" is not a valid choice.`},
		{`say "hi"`, `"say \"hi\"" is not a valid choice.`},
		{`back\slash`, `"back\\slash" is not a valid choice.`},
		{"line\nbreak", `"line\nbreak" is not a valid choice.`},
		{42.0, `"42" is not a valid choice.`},
	} {
		if got := invalidChoice(test.value); got != test.want {
			t.Errorf("invalidChoice(%#v) = %s, want %s", test.value, got, test.want)
		}
	}
}
