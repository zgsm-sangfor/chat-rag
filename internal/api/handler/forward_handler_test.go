package handler

import (
	"net/http"
	"testing"
)

func TestCopyForwardHeadersRemovesSensitiveHeaders(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer secret")
	headers.Set("X-Apikey", "costrict_team_secret")
	headers.Set("X-Api-Key", "costrict_team_secret")
	headers.Set("Cookie", "sid=secret")
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Request-Id", "request-1")

	dst := make(map[string]string)
	copyForwardHeaders(dst, headers)

	if len(dst) != 2 {
		t.Fatalf("copied headers = %#v, want only non-sensitive headers", dst)
	}
	if dst["Content-Type"] != "application/json" || dst["X-Request-Id"] != "request-1" {
		t.Fatalf("non-sensitive headers were not preserved: %#v", dst)
	}
	for _, name := range []string{"Authorization", "X-Apikey", "X-Api-Key", "Cookie"} {
		if _, ok := dst[name]; ok {
			t.Fatalf("sensitive header %q was persisted", name)
		}
	}
}
