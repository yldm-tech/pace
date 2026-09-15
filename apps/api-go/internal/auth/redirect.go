package auth

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func validateNextPath(nextPath string) string {
	if nextPath == "" || len(nextPath) > 500 {
		return ""
	}
	nextPath = strings.ReplaceAll(nextPath, `\`, "")
	parsed, err := url.Parse(nextPath)
	if err != nil {
		return ""
	}
	if parsed.IsAbs() || parsed.Host != "" {
		nextPath = parsed.Path
	}
	if !strings.HasPrefix(nextPath, "/") || strings.Contains(nextPath, "..") {
		return ""
	}
	lower := strings.ToLower(nextPath)
	for _, suspicious := range []string{
		"javascript:", "data:", "vbscript:", "file:", "ftp:", "%2e%2e", "%2f%2f", "%5c%5c",
		"<script", "<iframe", "<object", "<embed", "<form", "onload=", "onerror=", "onclick=",
	} {
		if strings.Contains(lower, suspicious) {
			return ""
		}
	}
	return nextPath
}

func safeRedirectURL(baseURL, nextPath string, authenticationError *Error) string {
	baseURL = strings.TrimRight(baseURL, "/")
	parts := make([]string, 0, 4)
	if validated := validateNextPath(nextPath); validated != "" {
		// Django intentionally keeps next_path as a query parameter instead of
		// navigating to it directly; the web client consumes it after auth.
		parts = append(parts, "next_path="+validated)
	}
	if authenticationError != nil {
		values := url.Values{}
		values.Set("error_code", strconv.Itoa(authenticationError.Code))
		values.Set("error_message", authenticationError.Message)
		for key, value := range authenticationError.Payload {
			values.Set(key, fmt.Sprint(value))
		}
		parts = append(parts, values.Encode())
	}
	if len(parts) == 0 {
		return baseURL
	}
	return baseURL + "/?" + strings.Join(parts, "&")
}

func spaceSuccessURL(baseURL, nextPath string) string {
	return strings.TrimRight(baseURL, "/") + validateNextPath(nextPath)
}
