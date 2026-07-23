package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenValid(t *testing.T) {
	if !validToken(signToken(time.Now().Add(time.Hour).Unix())) {
		t.Error("a fresh token should validate")
	}
}

func TestTokenExpired(t *testing.T) {
	if validToken(signToken(time.Now().Add(-time.Minute).Unix())) {
		t.Error("an expired token must not validate")
	}
}

func TestTokenTampered(t *testing.T) {
	tok := signToken(time.Now().Add(time.Hour).Unix())
	last := tok[len(tok)-1]
	flip := byte('a')
	if last == 'a' {
		flip = 'b'
	}
	if validToken(tok[:len(tok)-1] + string(flip)) {
		t.Error("a tampered signature must not validate")
	}
	for _, bad := range []string{"", "garbage", "123", "9999999999.deadbeef"} {
		if validToken(bad) {
			t.Errorf("malformed token %q must not validate", bad)
		}
	}
}

func TestRequireAuth(t *testing.T) {
	h := requireAuth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	// no cookie -> 401
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/api/apps", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no cookie: want 401, got %d", rec.Code)
	}

	// valid cookie -> handler runs
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/apps", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: signToken(time.Now().Add(time.Hour).Unix())})
	h(rec, req)
	if rec.Code != 204 {
		t.Errorf("valid cookie: want 204, got %d", rec.Code)
	}
}
