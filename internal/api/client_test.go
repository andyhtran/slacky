package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientSlackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token")
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": false, "error": "missing_scope"})
	}))
	defer server.Close()

	client := testClient(server)
	var out map[string]any
	err := client.Call(context.Background(), "search.messages", nil, &out)
	var slackErr SlackError
	if !errors.As(err, &slackErr) {
		t.Fatalf("expected SlackError, got %T %v", err, err)
	}
	if slackErr.Code != "missing_scope" {
		t.Fatalf("unexpected Slack error code: %s", slackErr.Code)
	}
}

func TestClientRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Retry-After", "2")
		http.Error(writer, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := testClient(server)
	client.MaxRateLimitWait = 0
	var out map[string]any
	err := client.Call(context.Background(), "search.messages", nil, &out)
	var rateLimit RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("expected RateLimitError, got %T %v", err, err)
	}
	if rateLimit.Method != "search.messages" || rateLimit.RetryAfter != 2*time.Second {
		t.Fatalf("unexpected rate limit: %#v", rateLimit)
	}
}

func TestClientRateLimitRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if calls == 1 {
			writer.Header().Set("Retry-After", "0")
			http.Error(writer, "rate limited", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "value": "done"})
	}))
	defer server.Close()

	client := testClient(server)
	client.MaxRateLimitWait = time.Second
	var out map[string]any
	if err := client.Call(context.Background(), "auth.test", nil, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if out["value"] != "done" {
		t.Fatalf("unexpected response: %#v", out)
	}
}

func TestAuthTestIncludesMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-OAuth-Scopes", "search:read,channels:read")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"ok":      true,
			"url":     "https://example.slack.com/",
			"team":    "Example",
			"user":    "alice",
			"team_id": "T123",
			"user_id": "U123",
		})
	}))
	defer server.Close()

	result, err := testClient(server).AuthTest(context.Background())
	if err != nil {
		t.Fatalf("auth.test: %v", err)
	}
	if result.TeamID != "T123" || result.UserID != "U123" {
		t.Fatalf("unexpected auth.test identity: %#v", result)
	}
	if len(result.Scopes) != 2 || result.Scopes[0] != "search:read" || result.Scopes[1] != "channels:read" {
		t.Fatalf("unexpected scopes: %#v", result.Scopes)
	}
}

func TestClientActiveCooldownSkipsRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
	}))
	defer server.Close()

	client := testClient(server)
	client.Cooldown = func(method string) (RateLimitError, bool) {
		return RateLimitError{Method: method, RetryAfter: time.Minute, RetryAt: time.Now().Add(time.Minute)}, true
	}
	var out map[string]any
	err := client.Call(context.Background(), "search.messages", nil, &out)
	var rateLimit RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("expected RateLimitError, got %T %v", err, err)
	}
	if called {
		t.Fatalf("server should not have been called under active cooldown")
	}
}

func testClient(server *httptest.Server) *Client {
	return &Client{
		Token:            "token",
		UserAgent:        "slacky-test",
		HTTPClient:       server.Client(),
		MaxRateLimitWait: time.Second,
		BaseURL:          server.URL + "/",
	}
}
