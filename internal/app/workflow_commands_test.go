package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andyhtran/slacky/internal/api"
)

func TestWorkflowCommandRunsLocal(t *testing.T) {
	tests := []struct {
		name   string
		run    func(*Globals) error
		assert func(t *testing.T, envelope map[string]any)
	}{
		{
			name: "search local",
			run: func(globals *Globals) error {
				return (&SearchCmd{Query: []string{"cached"}, Local: true}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "cache")
				results := workflowJSONArray(t, envelope, "results")
				if len(results) == 0 {
					t.Fatalf("expected local search results, got %#v", envelope)
				}
				for _, result := range results {
					message := workflowJSONObject(t, result)
					if strings.Contains(workflowJSONString(message, "excerpt"), "cached") {
						return
					}
				}
				t.Fatalf("expected a cached excerpt, got %#v", results)
			},
		},
		{
			name: "find local",
			run: func(globals *Globals) error {
				return (&FindCmd{Topic: []string{"cached"}, Local: true}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "cache")
				threads := workflowJSONArray(t, envelope, "threads")
				if len(threads) == 0 {
					t.Fatalf("expected local ranked threads, got %#v", envelope)
				}
				search := workflowJSONObject(t, envelope["search"])
				if local, _ := search["local"].(bool); !local {
					t.Fatalf("expected search.local true, got %#v", search)
				}
			},
		},
		{
			name: "channels resolve cache",
			run: func(globals *Globals) error {
				return (&ChannelsCmd{Target: []string{"general"}}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "cache")
				channels := workflowJSONArray(t, envelope, "channels")
				if len(channels) != 1 {
					t.Fatalf("expected one channel, got %#v", channels)
				}
				channel := workflowJSONObject(t, channels[0])
				if workflowJSONString(channel, "id") != workflowChannelID || workflowJSONString(channel, "name") != "general" {
					t.Fatalf("unexpected channel: %#v", channel)
				}
			},
		},
		{
			name: "user resolve cache",
			run: func(globals *Globals) error {
				return (&UserCmd{Target: []string{"person"}}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "cache")
				user := workflowJSONObject(t, envelope["user"])
				if workflowJSONString(user, "id") != "U123" || workflowJSONString(user, "name") != "person" {
					t.Fatalf("unexpected user: %#v", user)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWorkflowCache(t)
			envelope := runWorkflowCommandJSON(t, test.run)
			test.assert(t, envelope)
		})
	}
}

func TestWorkflowCommandRunsLive(t *testing.T) {
	tests := []struct {
		name   string
		run    func(*Globals) error
		assert func(t *testing.T, envelope map[string]any)
	}{
		{
			name: "search live",
			run: func(globals *Globals) error {
				return (&SearchCmd{Query: []string{"release"}}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "slack")
				results := workflowJSONArray(t, envelope, "results")
				if len(results) != 2 {
					t.Fatalf("expected two live search results, got %#v", results)
				}
			},
		},
		{
			name: "find live",
			run: func(globals *Globals) error {
				return (&FindCmd{Topic: []string{"release"}}).Run(globals)
			},
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				assertWorkflowSource(t, envelope, "slack")
				threads := workflowJSONArray(t, envelope, "threads")
				if len(threads) == 0 {
					t.Fatalf("expected ranked live threads, got %#v", envelope)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupWorkflowCache(t)
			withWorkflowAPIServer(t, workflowLiveHandler(t, false))
			envelope := runWorkflowCommandJSON(t, test.run)
			test.assert(t, envelope)
		})
	}
}

func TestWorkflowCommandRunsUserNotFoundErrorEnvelope(t *testing.T) {
	setupWorkflowCache(t)
	withWorkflowAPIServer(t, workflowLiveHandler(t, true))

	envelope := runWorkflowCommandJSONAllowError(t, func(globals *Globals) error {
		return (&UserCmd{Target: []string{"nomatch"}}).Run(globals)
	})
	if ok, _ := envelope["ok"].(bool); ok {
		t.Fatalf("expected not-found error envelope, got %#v", envelope)
	}
	errorObject := workflowJSONObject(t, envelope["error"])
	if workflowJSONString(errorObject, "kind") != "user_not_found" {
		t.Fatalf("unexpected error envelope: %#v", envelope)
	}
}

func runWorkflowCommandJSON(t *testing.T, run func(*Globals) error) map[string]any {
	t.Helper()

	globals := &Globals{JSON: true, Timeout: time.Second}
	var runErr error
	text := captureStdout(t, func() {
		runErr = run(globals)
	})
	if runErr != nil {
		t.Fatalf("run command: %v", runErr)
	}
	envelope := decodeWorkflowCommandEnvelope(t, text)
	if ok, _ := envelope["ok"].(bool); !ok {
		t.Fatalf("expected ok envelope, got %#v", envelope)
	}
	return envelope
}

func runWorkflowCommandJSONAllowError(t *testing.T, run func(*Globals) error) map[string]any {
	t.Helper()

	globals := &Globals{JSON: true, Timeout: time.Second}
	text := captureStdout(t, func() {
		if err := run(globals); err != nil {
			handleRunError(globals, err)
		}
	})
	return decodeWorkflowCommandEnvelope(t, text)
}

func decodeWorkflowCommandEnvelope(t *testing.T, text string) map[string]any {
	t.Helper()

	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, text)
	}
	return envelope
}

func withWorkflowAPIServer(t *testing.T, handler http.Handler) {
	t.Helper()

	server := httptest.NewServer(handler)
	oldNewAPIClient := newAPIClient
	newAPIClient = func(token string, version string, timeout time.Duration, maxRateLimitWait time.Duration) *api.Client {
		client := api.NewClient(token, version, timeout, maxRateLimitWait)
		client.BaseURL = server.URL + "/"
		return client
	}
	t.Cleanup(func() {
		newAPIClient = oldNewAPIClient
		server.Close()
	})
}

func workflowLiveHandler(t *testing.T, emptyUsers bool) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search.messages":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"messages": {
					"total": 2,
					"pagination": {"page": 1, "page_count": 1},
					"matches": [
						{
							"type": "message",
							"user": "U123",
							"username": "sampleuser",
							"text": "release planning root",
							"ts": "1717550000.000000",
							"permalink": "https://example.slack.com/archives/C123/p1717550000000000",
							"channel": {"id": "C123", "name": "general"}
						},
						{
							"type": "message",
							"user": "U123",
							"username": "sampleuser",
							"text": "release reply detail",
							"ts": "1717550000.000100",
							"thread_ts": "1717550000.000000",
							"permalink": "https://example.slack.com/archives/C123/p1717550000000100?thread_ts=1717550000.000000",
							"channel": {"id": "C123", "name": "general"}
						}
					]
				}
			}`))
		case "/users.list":
			if emptyUsers {
				_, _ = w.Write([]byte(`{"ok": true, "members": [], "response_metadata": {"next_cursor": ""}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"ok": true,
				"members": [{
					"id": "U123",
					"team_id": "T123",
					"name": "sampleuser",
					"real_name": "Sample User",
					"profile": {"display_name": "sampleuser", "email": "person@example.com"}
				}],
				"response_metadata": {"next_cursor": ""}
			}`))
		case "/users.info":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"user": {
					"id": "U123",
					"team_id": "T123",
					"name": "sampleuser",
					"real_name": "Sample User",
					"profile": {"display_name": "sampleuser", "email": "person@example.com"}
				}
			}`))
		case "/conversations.info":
			_, _ = w.Write([]byte(`{
				"ok": true,
				"channel": {"id": "C123", "name": "general", "is_channel": true}
			}`))
		default:
			t.Fatalf("unexpected Slack API path %s", r.URL.Path)
		}
	})
}

func assertWorkflowSource(t *testing.T, envelope map[string]any, want string) {
	t.Helper()

	if got := workflowJSONString(envelope, "source"); got != want {
		t.Fatalf("source = %q, want %q; envelope=%#v", got, want, envelope)
	}
}

func workflowJSONArray(t *testing.T, envelope map[string]any, key string) []any {
	t.Helper()

	items, ok := envelope[key].([]any)
	if !ok {
		t.Fatalf("%s should be an array: %#v", key, envelope[key])
	}
	return items
}

func workflowJSONObject(t *testing.T, value any) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %#v", value)
	}
	return object
}

func workflowJSONString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
