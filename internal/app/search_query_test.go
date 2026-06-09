package app

import (
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/api"
)

func TestExpandSearchUserHandles(t *testing.T) {
	resolve := func(handle string) (api.UserResult, bool) {
		if handle != "sample" {
			return api.UserResult{}, false
		}
		return api.UserResult{ID: "U123", Name: "sample"}, true
	}

	query, expansions := expandSearchUserHandles("@sample from:@sample person@example.com", resolve)
	want := "<@U123> from:<@U123> person@example.com"
	if query != want {
		t.Fatalf("expanded query = %q, want %q", query, want)
	}
	if len(expansions) != 1 || expansions[0].Handle != "@sample" || expansions[0].UserID != "U123" {
		t.Fatalf("unexpected expansions: %#v", expansions)
	}
}

func TestExpandSearchUserHandlesAcceptsFromWithoutAtAndUserID(t *testing.T) {
	resolve := func(handle string) (api.UserResult, bool) {
		if handle != "sample" {
			return api.UserResult{}, false
		}
		return api.UserResult{ID: "U123", RealName: "sample"}, true
	}

	query, expansions := expandSearchUserHandles("from:sample from:U456 U789", resolve)
	want := "from:<@U123> from:<@U456> <@U789>"
	if query != want {
		t.Fatalf("expanded query = %q, want %q", query, want)
	}
	if len(expansions) != 3 {
		t.Fatalf("expected three expansions, got %#v", expansions)
	}
}

func TestExpandSearchUserHandlesAcceptsBareSingleUserQuery(t *testing.T) {
	resolve := func(handle string) (api.UserResult, bool) {
		if handle != "samplealias" {
			return api.UserResult{}, false
		}
		return api.UserResult{ID: "U123", Name: "samplealias", RealName: "sample"}, true
	}

	query, expansions := expandSearchUserHandles("samplealias", resolve)
	if query != "<@U123>" {
		t.Fatalf("expanded bare query = %q", query)
	}
	if len(expansions) != 1 || expansions[0].Handle != "@samplealias" || expansions[0].Name != "sample" {
		t.Fatalf("unexpected expansions: %#v", expansions)
	}
}

func TestExpandSearchUserHandlesDoesNotRewriteMultiWordText(t *testing.T) {
	query, expansions := expandSearchUserHandles("samplealias rollout", func(string) (api.UserResult, bool) {
		return api.UserResult{ID: "U123", Name: "samplealias", RealName: "sample"}, true
	})
	if query != "samplealias rollout" {
		t.Fatalf("multi-word text should not be rewritten: %q", query)
	}
	if len(expansions) != 0 {
		t.Fatalf("unexpected expansions: %#v", expansions)
	}
}

func TestExpandSearchUserHandlesLeavesUnknownHandles(t *testing.T) {
	query, expansions := expandSearchUserHandles("@unknown", func(string) (api.UserResult, bool) {
		return api.UserResult{}, false
	})
	if query != "@unknown" {
		t.Fatalf("unknown handle should not be rewritten: %q", query)
	}
	if len(expansions) != 0 {
		t.Fatalf("unexpected expansions: %#v", expansions)
	}
}

func TestExpandSearchChannels(t *testing.T) {
	query, expansions := expandSearchChannels("in:#genral smoke in:C123", func(target string) (api.ChannelResult, bool) {
		switch target {
		case "#genral":
			return api.ChannelResult{ID: "C123", Name: "general"}, true
		case "C123":
			return api.ChannelResult{ID: "C123", Name: "general"}, true
		default:
			return api.ChannelResult{}, false
		}
	})
	want := "in:general smoke in:general"
	if query != want {
		t.Fatalf("expanded channel query = %q, want %q", query, want)
	}
	if len(expansions) != 2 {
		t.Fatalf("unexpected expansions: %#v", expansions)
	}
}

func TestMatchUserHandleUsesUniqueVisibleName(t *testing.T) {
	users := []api.UserResult{
		{ID: "U123", Name: "samplealias", RealName: "sample"},
	}
	user, ok := matchUserHandle(users, "sample")
	if !ok || user.ID != "U123" {
		t.Fatalf("expected unique real-name match, got ok=%t user=%#v", ok, user)
	}
}

func TestMatchUserHandleUsesUniqueFuzzyName(t *testing.T) {
	users := []api.UserResult{
		{ID: "U123", Name: "samplealias", RealName: "sample"},
	}
	user, ok := matchUserHandle(users, "smaple")
	if !ok || user.ID != "U123" {
		t.Fatalf("expected fuzzy handle match, got ok=%t user=%#v", ok, user)
	}
}

func TestMatchUserHandleUsesUniquePartialName(t *testing.T) {
	users := []api.UserResult{
		{ID: "U123", Name: "sample.person", RealName: "Sample Person"},
		{ID: "U456", Name: "placeholder.user", RealName: "Placeholder User"},
	}
	user, ok := matchUserHandle(users, "sam")
	if !ok || user.ID != "U123" {
		t.Fatalf("expected partial visible-name match, got ok=%t user=%#v", ok, user)
	}
}

func TestMatchUserHandleSkipsAmbiguousVisibleName(t *testing.T) {
	users := []api.UserResult{
		{ID: "U123", Name: "sampleone", RealName: "sample"},
		{ID: "U456", Name: "sampletwo", DisplayName: "sample"},
	}
	user, ok := matchUserHandle(users, "sample")
	if ok {
		t.Fatalf("ambiguous visible name should not resolve, got %#v", user)
	}
}

func TestMatchUserHandleReturnsSuggestionsForAmbiguousPartialName(t *testing.T) {
	users := []api.UserResult{
		{ID: "U123", Name: "sample.one", RealName: "Sample One"},
		{ID: "U456", Name: "sample.two", RealName: "Sample Two"},
	}
	_, suggestions, ok := matchUserHandleWithSuggestions(users, "sample")
	if ok {
		t.Fatalf("ambiguous partial should not resolve")
	}
	if len(suggestions) != 2 {
		t.Fatalf("expected suggestions, got %#v", suggestions)
	}
}

func TestFilterChannelsHandlesSlackMentionAndFuzzyName(t *testing.T) {
	channels := []api.ChannelResult{
		{ID: "C123", Name: "general"},
		{ID: "C456", Name: "social"},
	}
	if got := filterChannels(channels, "<#C123|general>"); len(got) != 1 || got[0].ID != "C123" {
		t.Fatalf("Slack channel mention did not resolve: %#v", got)
	}
	if got := filterChannels(channels, "genral"); len(got) != 1 || got[0].ID != "C123" {
		t.Fatalf("fuzzy channel name did not resolve: %#v", got)
	}
}

func TestCompactMessageResultsIncludeRootAwareCommands(t *testing.T) {
	results := compactMessageResults([]api.MessageResult{{
		ChannelID: "C123",
		TS:        "1717440000.000100",
		RootTS:    "1717440000.000000",
		Permalink: "https://example.slack.com/archives/C123/p1717440000000100?thread_ts=1717440000.000000",
		Excerpt:   "reply text",
	}})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	commands := results[0].Commands
	if commands.Thread != "slacky thread --channel C123 --ts 1717440000.000000" {
		t.Fatalf("thread command = %q", commands.Thread)
	}
	if commands.Context != "slacky context --channel C123 --ts 1717440000.000100" {
		t.Fatalf("context command = %q", commands.Context)
	}
	if commands.RootContext != "slacky context --channel C123 --ts 1717440000.000000" {
		t.Fatalf("root context command = %q", commands.RootContext)
	}
	if commands.Open == "" {
		t.Fatalf("expected open command")
	}
}

func TestCompactMessageResultsDoNotSynthesizeThreadTS(t *testing.T) {
	results := compactMessageResults([]api.MessageResult{{
		ChannelID: "C123",
		TS:        "1717440000.000000",
		Excerpt:   "standalone text",
	}})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	if results[0].ThreadTS != "" {
		t.Fatalf("thread_ts should not be synthesized for standalone messages, got %q", results[0].ThreadTS)
	}
	if results[0].RootTS != "1717440000.000000" {
		t.Fatalf("root_ts = %q", results[0].RootTS)
	}
}

func TestRenderMessageListReadable(t *testing.T) {
	text := renderMessageListWithOptions("Search", []string{"Query: whole"}, []api.MessageResult{{
		ChannelID:   "C123",
		ChannelName: "general",
		TS:          "1717440000.000000",
		RootTS:      "1717440000.000000",
		Permalink:   "https://example.slack.com/archives/C123/p1717440000000000",
		User:        "U123",
		Username:    "person",
		Excerpt:     "Release notes mention the \uE000whole\uE001 checklist and include enough surrounding words to wrap cleanly.",
	}}, messageRenderOptions{})
	for _, want := range []string{
		"1. #general  @person",
		"Text:",
		"whole checklist",
		"Thread:\n     slacky thread --channel C123 --ts 1717440000.000000",
		"Context:\n     slacky context --channel C123 --ts 1717440000.000000",
		"slacky search --json <query>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered text missing %q:\n%s", want, text)
		}
	}
	for _, reject := range []string{"\uE000", "\uE001", "Permalink:"} {
		if strings.Contains(text, reject) {
			t.Fatalf("rendered text should not contain %q:\n%s", reject, text)
		}
	}
}

func TestRenderMessageListVerboseRichContent(t *testing.T) {
	message := api.MessageResult{
		ChannelID:   "C123",
		ChannelName: "general",
		TS:          "1717440000.000000",
		RootTS:      "1717440000.000000",
		User:        "U123",
		Username:    "person",
		TextFull:    "Main body",
		Excerpt:     "Visible text ``` hidden code ``` after",
		Blocks:      []byte(`[{"type":"section","text":{"type":"mrkdwn","text":"Block body"}}]`),
		Attachments: []byte(`[{"fallback":"Attachment fallback","fields":[{"title":"Risk","value":"database migration"}]}]`),
		Files:       []api.FileResult{{Title: "Launch spec", Filetype: "pdf", Size: 2048, Permalink: "https://example.slack.com/files/F123"}},
	}
	defaultText := renderMessageListWithOptions("Message", nil, []api.MessageResult{message}, messageRenderOptions{})
	if strings.Contains(defaultText, "hidden code") {
		t.Fatalf("default excerpt should strip code fences:\n%s", defaultText)
	}

	verboseText := renderMessageListWithOptions("Message", nil, []api.MessageResult{message}, messageRenderOptions{Verbose: true, IncludeRichContent: true})
	for _, want := range []string{
		"Main body",
		"--- blocks ---",
		"Block body",
		"--- attachments ---",
		"Risk: database migration",
		"--- files ---",
		"File: Launch spec (pdf, 2048 bytes) https://example.slack.com/files/F123",
	} {
		if !strings.Contains(verboseText, want) {
			t.Fatalf("verbose rich output missing %q:\n%s", want, verboseText)
		}
	}
}

func TestRenderSearchMessageListCompact(t *testing.T) {
	text := renderSearchMessageList([]string{"Query: whole", "Showing: 2 of 2"}, []api.MessageResult{
		{
			ChannelID:   "C123",
			ChannelName: "general",
			TS:          "1717440000.000000",
			RootTS:      "1717440000.000000",
			User:        "U123",
			Username:    "person",
			Excerpt:     "Release notes mention the whole checklist.",
		},
		{
			ChannelID:   "C456",
			ChannelName: "team",
			TS:          "1717440100.000000",
			RootTS:      "1717440100.000000",
			User:        "U456",
			Username:    "teammate",
			Excerpt:     "Whole launch plan is ready.",
		},
	}, "whole", []string{"whole"})
	for _, want := range []string{
		"REF  CHANNEL",
		"1    #general",
		"2    #team",
		"Open:",
		"slacky thread --channel C123 --ts 1717440000.000000  # 1",
		"Context:",
		"slacky context --channel C456 --ts 1717440100.000000  # 2",
		"More:",
		"slacky search whole --evidence",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compact search output missing %q:\n%s", want, text)
		}
	}
	for _, reject := range []string{"Text:", "   Thread:\n", "   Context:\n"} {
		if strings.Contains(text, reject) {
			t.Fatalf("compact search output should not contain %q:\n%s", reject, text)
		}
	}
}

func TestMessagesWithDisplayMentionsUsesResolvedLabels(t *testing.T) {
	messages := []api.MessageResult{{Excerpt: "<@U123> and @U123"}}
	display := messagesWithDisplayMentions(messages, map[string]string{"U123": "@sample"})
	if display[0].Excerpt != "@sample and @sample" {
		t.Fatalf("expanded display excerpt = %q", display[0].Excerpt)
	}
	if messages[0].Excerpt != "<@U123> and @U123" {
		t.Fatalf("original message should remain unchanged: %#v", messages[0])
	}
}

func TestMessagesWithDisplayMentionsResolvesSender(t *testing.T) {
	messages := []api.MessageResult{{
		User:    "U123",
		Excerpt: "Mentioned @U123 in text",
	}}
	display := messagesWithDisplayMentions(messages, map[string]string{"U123": "@sample"})
	if display[0].Username != "sample" || display[0].Excerpt != "Mentioned @sample in text" {
		t.Fatalf("display message = %#v", display[0])
	}
}

func TestUserMentionLabelPrefersVisibleName(t *testing.T) {
	user := api.UserResult{ID: "U123", Name: "samplealias", RealName: "sample"}
	if got := userMentionLabel(user); got != "sample" {
		t.Fatalf("user label = %q", got)
	}
}

func TestMessagesWithDisplayChannelsFillsChannelName(t *testing.T) {
	messages := []api.MessageResult{{ChannelID: "C123", Excerpt: "hello"}}
	display := messagesWithDisplayChannels(messages, map[string]string{"C123": "general"})
	if display[0].ChannelName != "general" {
		t.Fatalf("display message = %#v", display[0])
	}
	if messages[0].ChannelName != "" {
		t.Fatalf("original message should remain unchanged: %#v", messages[0])
	}
}

func TestRenderThreadListUsesCopyableOpenBlock(t *testing.T) {
	text := renderThreadListWithOptions("Thread", nil, []api.ThreadResult{{
		ChannelID:   "C123",
		ChannelName: "general",
		RootTS:      "1717440000.000000",
		Messages:    []api.MessageResult{{Excerpt: "hello"}},
	}}, messageRenderOptions{})
	if !strings.Contains(text, "1. #general") {
		t.Fatalf("thread title should prefer channel name:\n%s", text)
	}
	if !strings.Contains(text, "Open:\n     slacky thread --channel C123 --ts 1717440000.000000") {
		t.Fatalf("open command should be on its own line:\n%s", text)
	}
	if strings.Contains(text, "Open: slacky") {
		t.Fatalf("open command should not be inline:\n%s", text)
	}
}

func TestIsEmailTarget(t *testing.T) {
	if !isEmailTarget("person@example.com") {
		t.Fatalf("expected email target")
	}
	for _, value := range []string{"@person", "person", "U123"} {
		if isEmailTarget(value) {
			t.Fatalf("did not expect %q to be treated as email", value)
		}
	}
}
