package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// APILogPublisher queues the record of one request made with an api key.
type APILogPublisher interface {
	PublishAPIActivityLog(ctx context.Context, data map[string]any) error
}

// sensitiveHeaders are the three whose values are never written down. The key itself is one of them, which is why the record identifies a key by a digest instead.
var sensitiveHeaders = map[string]bool{"x-api-key": true, "authorization": true, "cookie": true}

// apiActivityLog records every request made with an api key, which is what an installation's administrator reads to see what a key has been doing.
//
// It runs for every request and leaves without doing anything when there is no key, which is what the Django middleware does. Until now the Go side did neither: a request the proxy had cut over was served without ever being recorded, so a key's history had a hole in it wherever the migration had reached. This closes that.
func apiActivityLog(publisher APILogPublisher, secretKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-Api-Key")
		if apiKey == "" || publisher == nil {
			c.Next()
			return
		}

		// The body has to be read here and put back, since the handler has not seen it yet.
		var requestBody []byte
		if c.Request.Body != nil {
			requestBody, _ = io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
		}
		recorder := &responseRecorder{ResponseWriter: c.Writer}
		c.Writer = recorder

		c.Next()

		digest := hmac.New(sha256.New, []byte(secretKey))
		digest.Write([]byte(apiKey))
		data := map[string]any{
			// The key is turned into a stable digest rather than written down, and the digest is keyed so it cannot be worked out from a key somebody already has.
			"token_identifier": hex.EncodeToString(digest.Sum(nil)),
			"path":             c.Request.URL.Path,
			"method":           c.Request.Method,
			"query_params":     c.Request.URL.RawQuery,
			"headers":          renderRequestHeaders(c.Request.Header),
			"body":             textOrNothing(requestBody),
			"response_body":    textOrNothing(recorder.body.Bytes()),
			"response_code":    recorder.Status(),
			"ip_address":       clientIP(c.Request),
			"user_agent":       headerOrNil(c.Request.Header, "User-Agent"),
		}
		// A record that cannot be queued is not worth failing the request for; the request itself has already been answered.
		_ = publisher.PublishAPIActivityLog(c.Request.Context(), data)
	}
}

// responseRecorder keeps a copy of what was written so it can be recorded beside the request.
type responseRecorder struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (recorder *responseRecorder) Write(payload []byte) (int, error) {
	recorder.body.Write(payload)
	return recorder.ResponseWriter.Write(payload)
}

func (recorder *responseRecorder) WriteString(payload string) (int, error) {
	recorder.body.WriteString(payload)
	return recorder.ResponseWriter.WriteString(payload)
}

// renderRequestHeaders writes the headers the way a python dict prints, which is the shape the column already holds. The three sensitive ones are replaced rather than dropped, so a reader can see that a key was sent without seeing which.
//
// Django writes them in the order the server handed them over; Go keeps headers in a map with no order at all, so they are written in name order instead. The set and the values are the same either way.
func renderRequestHeaders(header http.Header) string {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	rendered := make([]string, 0, len(names))
	for _, name := range names {
		value := strings.Join(header.Values(name), ",")
		if sensitiveHeaders[strings.ToLower(name)] {
			value = "[REDACTED]"
		}
		rendered = append(rendered, pythonRepr(name)+": "+pythonRepr(value))
	}
	return "{" + strings.Join(rendered, ", ") + "}"
}

// pythonRepr writes a string the way repr does, since the column holds what str() made of a dict of them. A value carrying an apostrophe is quoted with double quotes rather than escaped, which is the rule repr follows.
func pythonRepr(value string) string {
	quote := "'"
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = `"`
	}
	escaped := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`, quote, `\`+quote).Replace(value)
	return quote + escaped + quote
}

// binarySignatures are the three Django recognises by their first bytes and declines to decode at all.
var binarySignatures = [][]byte{[]byte("\x89PNG"), {0xff, 0xd8, 0xff}, []byte("%PDF")}

// textOrNothing keeps an empty body as null rather than as an empty string, which is what the column holds today.
func textOrNothing(payload []byte) any {
	if len(payload) == 0 {
		return nil
	}
	for _, signature := range binarySignatures {
		if bytes.HasPrefix(payload, signature) {
			return "[Binary Content]"
		}
	}
	if !utf8.Valid(payload) {
		// Django answers this with a sentence rather than the bytes, since the column is text.
		return "[Could not decode content]"
	}
	// A null byte survives decoding but cannot go into a Postgres text column, so the row is lost when the worker tries to write it. That is what happens upstream too, and it is reproduced rather than corrected here.
	return string(payload)
}

// clientIP is get_client_ip: the first address a proxy named, and failing that whoever connected. The address is not trimmed, so a header written with a space after the comma leaves one on the second address rather than on the first.
func clientIP(request *http.Request) any {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return first
	}
	// REMOTE_ADDR is the address alone, which is what SplitHostPort leaves.
	if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
		return host
	}
	if request.RemoteAddr == "" {
		return nil
	}
	return request.RemoteAddr
}

func headerOrNil(header http.Header, name string) any {
	value := header.Get(name)
	if value == "" {
		return nil
	}
	return value
}
