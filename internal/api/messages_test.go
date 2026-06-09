package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCleanSlackTextRemovesSearchHighlightMarkers(t *testing.T) {
	got := CleanSlackText("<@U123> shared \uE000whole\uE001 notes in <#C123|general>: <https://example.com|doc>")
	want := "@U123 shared whole notes in #general: doc (https://example.com)"
	if got != want {
		t.Fatalf("CleanSlackText() = %q, want %q", got, want)
	}
}

func TestSearchMessageResultFallsBackToBlockText(t *testing.T) {
	message := searchMessage{
		TS:      "1.000000",
		Channel: searchChannel{ID: "C123", Name: "general"},
		Blocks:  json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"Block hit text"}}]`),
	}
	result := message.result(false)
	if result.Excerpt != "Block hit text" {
		t.Fatalf("excerpt = %q, want block text", result.Excerpt)
	}
	if len(result.Blocks) != 0 {
		t.Fatalf("blocks should stay omitted without includeRichContent: %s", result.Blocks)
	}
}

func TestSearchMessageResultInfersThreadTSFromPermalink(t *testing.T) {
	message := searchMessage{
		TS:        "1.000001",
		Permalink: "https://example.slack.com/archives/C123/p1000001?thread_ts=1.000000&cid=C123",
		Channel:   searchChannel{ID: "C123", Name: "general"},
		Text:      "reply hit",
	}
	result := message.result(false)
	if result.ThreadTS != "1.000000" || result.RootTS != "1.000000" {
		t.Fatalf("thread/root ts = %q/%q, want inferred root", result.ThreadTS, result.RootTS)
	}
}

func TestRichTextFromPartsRendersSlackBlocksAttachmentsAndFiles(t *testing.T) {
	blocks := json.RawMessage(`[
		{"type":"header","text":{"type":"plain_text","text":"Launch Review"}},
		{"type":"section","text":{"type":"mrkdwn","text":"Review <@U123> in <#C123|general>"},"fields":[{"type":"mrkdwn","text":"Owner"},{"type":"mrkdwn","text":"Platform"}]},
		{"type":"context","elements":[{"type":"mrkdwn","text":"Context note"},{"type":"plain_text","text":"plain bit"}]},
		{"type":"actions","elements":[{"type":"button","text":{"type":"plain_text","text":"Approve launch"}}]},
		{"type":"image","title":{"type":"plain_text","text":"Architecture diagram"},"alt_text":"Architecture alt"},
		{"type":"rich_text","elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"See "},{"type":"link","url":"https://example.com","text":"runbook"},{"type":"emoji","name":"rocket"}]},
			{"type":"rich_text_list","style":"bullet","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"First bullet"}]}]},
			{"type":"rich_text_list","style":"ordered","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"First ordered"}]}]},
			{"type":"rich_text_quote","elements":[{"type":"text","text":"quoted text"}]},
			{"type":"rich_text_preformatted","elements":[{"type":"text","text":"code sample"}]}
		]},
		{"type":"table","rows":[{"cells":[{"text":"Service"},{"text":"Status"}]},{"cells":[{"text":"API"},{"text":"Ready"}]}]}
	]`)
	attachments := json.RawMessage(`[{
		"fallback":"Fallback text",
		"title":"Attachment title",
		"text":"Attachment body",
		"fields":[{"title":"Risk","value":"database migration"}],
		"actions":[{"text":"Open ticket"}]
	}]`)
	files := []FileResult{{
		ID:        "F123",
		Title:     "Launch spec",
		Filetype:  "pdf",
		Size:      2048,
		Permalink: "https://example.slack.com/files/F123",
	}}

	text := RichTextFromParts("", blocks, attachments, files)
	for _, want := range []string{
		"Launch Review",
		"Review @U123 in #general",
		"Owner | Platform",
		"Context note plain bit",
		"Approve launch",
		"Architecture diagram",
		"runbook (https://example.com)",
		":rocket:",
		"- First bullet",
		"1. First ordered",
		"Quote: quoted text",
		"``` code sample ```",
		"Service | Status",
		"Risk: database migration",
		"Open ticket",
		"File: Launch spec (pdf, 2048 bytes) https://example.slack.com/files/F123",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rich text missing %q:\n%s", want, text)
		}
	}
}

func TestSearchMessagesHydratesRichContent(t *testing.T) {
	repliesCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/search.messages":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": map[string]any{
					"total": 2,
					"matches": []map[string]any{
						{
							"type":      "message",
							"user":      "U123",
							"text":      "snippet",
							"ts":        "1.000001",
							"thread_ts": "1.000000",
							"permalink": "https://example.slack.com/archives/C123/p1000001?thread_ts=1.000000",
							"channel":   map[string]any{"id": "C123", "name": "general"},
						},
						{
							"type":      "message",
							"user":      "U456",
							"text":      "second snippet",
							"ts":        "1.000002",
							"thread_ts": "1.000000",
							"permalink": "https://example.slack.com/archives/C123/p1000002?thread_ts=1.000000",
							"channel":   map[string]any{"id": "C123", "name": "general"},
						},
					},
				},
			})
		case "/conversations.replies":
			repliesCalls++
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "user": "U999", "ts": "1.000000", "thread_ts": "1.000000", "text": "root"},
					{
						"type":      "message",
						"user":      "U123",
						"ts":        "1.000001",
						"thread_ts": "1.000000",
						"text":      "full reply",
						"blocks": []map[string]any{{
							"type": "section",
							"text": map[string]any{"type": "mrkdwn", "text": "Hydrated block text"},
						}},
						"files": []map[string]any{{
							"id":        "F123",
							"title":     "Hydrated spec",
							"filetype":  "pdf",
							"permalink": "https://example.slack.com/files/F123",
						}},
					},
					{
						"type":      "message",
						"user":      "U456",
						"ts":        "1.000002",
						"thread_ts": "1.000000",
						"text":      "second full reply",
						"blocks": []map[string]any{{
							"type": "section",
							"text": map[string]any{"type": "mrkdwn", "text": "Second hydrated block"},
						}},
					},
				},
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		case "/chat.getPermalink":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok":        true,
				"permalink": "https://example.slack.com/archives/C123/p1000000",
			})
		default:
			t.Fatalf("unexpected API path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	result, err := testClient(server).SearchMessages(context.Background(), "topic", 2, "score", true)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected two hydrated results, got %#v", result.Messages)
	}
	if repliesCalls != 1 {
		t.Fatalf("expected one thread hydration call, got %d", repliesCalls)
	}
	message := result.Messages[0]
	if len(message.Blocks) == 0 || len(message.Files) != 1 {
		t.Fatalf("expected blocks/files from hydrated payload, got %#v", message)
	}
	if !strings.Contains(message.Excerpt, "Hydrated block text") || !strings.Contains(message.Excerpt, "Hydrated spec") {
		t.Fatalf("expected hydrated rich excerpt, got %q", message.Excerpt)
	}
	if message.Permalink != "https://example.slack.com/archives/C123/p1000001?thread_ts=1.000000" {
		t.Fatalf("search permalink should be preserved, got %q", message.Permalink)
	}
	if !strings.Contains(result.Messages[1].Excerpt, "Second hydrated block") {
		t.Fatalf("expected second hit to hydrate from cached thread, got %q", result.Messages[1].Excerpt)
	}
}

func TestConversationsListPaginates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("cursor") {
		case "":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"id": "C123", "name": "general", "is_channel": true},
				},
				"response_metadata": map[string]string{"next_cursor": "next"},
			})
		case "next":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"channels": []map[string]any{
					{"id": "G456", "name": "private", "is_group": true, "is_private": true},
				},
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		default:
			t.Fatalf("unexpected cursor %q", request.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	channels, err := testClient(server).ConversationsList(context.Background(), 0)
	if err != nil {
		t.Fatalf("conversations list: %v", err)
	}
	if len(channels) != 2 || channels[1].ID != "G456" {
		t.Fatalf("expected paginated channels, got %#v", channels)
	}
}

func TestConversationHistoryPaginatesToCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("cursor") {
		case "":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "ts": "3.000000", "text": "third"},
					{"type": "message", "ts": "2.000000", "text": "second"},
				},
				"response_metadata": map[string]string{"next_cursor": "next"},
			})
		case "next":
			if request.URL.Query().Get("limit") != "1" {
				t.Fatalf("second page should request remaining limit, got %s", request.URL.Query().Get("limit"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "ts": "1.000000", "text": "first"},
				},
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		default:
			t.Fatalf("unexpected cursor %q", request.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	messages, err := testClient(server).ConversationHistory(context.Background(), "C123", 3, "", "", false)
	if err != nil {
		t.Fatalf("conversation history: %v", err)
	}
	if len(messages) != 3 || messages[2].TS != "1.000000" {
		t.Fatalf("expected paginated messages, got %#v", messages)
	}
}

func TestThreadPaginatesReplies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("cursor") {
		case "":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "ts": "1.000000", "thread_ts": "1.000000", "text": "root"},
					{"type": "message", "ts": "1.000001", "thread_ts": "1.000000", "text": "reply one"},
				},
				"response_metadata": map[string]string{"next_cursor": "next"},
			})
		case "next":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"ok": true,
				"messages": []map[string]any{
					{"type": "message", "ts": "1.000002", "thread_ts": "1.000000", "text": "reply two"},
				},
				"response_metadata": map[string]string{"next_cursor": ""},
			})
		default:
			t.Fatalf("unexpected cursor %q", request.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	thread, err := testClient(server).Thread(context.Background(), "C123", "1.000000", false)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if len(thread.Messages) != 3 || thread.RootTS != "1.000000" {
		t.Fatalf("expected paginated thread, got %#v", thread)
	}
}
