package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/resolve"
	"github.com/andyhtran/slacky/internal/store"
)

var (
	searchUserHandlePattern     = regexp.MustCompile(`(^|[\s(])((?:from:)?)@([A-Za-z0-9._-]+)`)
	searchFromUserPattern       = regexp.MustCompile(`(^|[\s(])from:([A-Za-z0-9._-]+)`)
	searchBareUserIDPattern     = regexp.MustCompile(`(^|[\s(])([UW][A-Z0-9]{2,})\b`)
	searchChannelPattern        = regexp.MustCompile(`(^|[\s(])in:(#[A-Za-z0-9._-]+|[CGD][A-Z0-9]{2,}|[A-Za-z0-9._-]+)`)
	slackChannelMentionPattern  = regexp.MustCompile(`^<#([A-Z0-9]+)(?:\|([^>]+))?>$`)
	searchAlreadyExpandedUserID = regexp.MustCompile(`^<@[UW][A-Z0-9]+>$`)
	displayUserMentionPattern   = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]+)?>|@([UW][A-Z0-9]{2,})\b`)
	slackCodeFencePattern       = regexp.MustCompile("(?s)```.*?```")
)

type messageRenderOptions struct {
	Verbose            bool
	IncludeRichContent bool
	Compact            bool
}

type SearchCmd struct {
	Query              []string `arg:"" optional:"" name:"query" help:"Slack search query"`
	Count              int      `help:"Maximum results; alias: --limit" default:"15" name:"count" aliases:"limit"`
	Sort               string   `help:"Slack search sort" enum:"score,timestamp" default:"score" name:"sort"`
	Verbose            bool     `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool     `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Compact            bool     `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
	Local              bool     `help:"Search only the local SQLite cache" name:"local"`
	GroupByThread      bool     `help:"Return ranked threads instead of individual hits" name:"group-by-thread"`
	Evidence           bool     `help:"Show detailed per-result evidence and commands" name:"evidence"`
}

type FindCmd struct {
	Topic              []string `arg:"" optional:"" name:"topic" help:"Natural-language topic"`
	Count              int      `help:"Maximum ranked threads; alias: --limit" default:"10" name:"count" aliases:"limit"`
	Verbose            bool     `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool     `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Compact            bool     `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
}

type MessageCmd struct {
	Channel            string `help:"Channel ID, #name, or name" name:"channel"`
	TS                 string `help:"Message timestamp" name:"ts"`
	Verbose            bool   `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool   `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Refresh            bool   `help:"Bypass cache and fetch from Slack" name:"refresh"`
	Compact            bool   `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
}

type ThreadCmd struct {
	Channel            string `help:"Channel ID, #name, or name" name:"channel"`
	TS                 string `help:"Parent or reply timestamp" name:"ts"`
	Verbose            bool   `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool   `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Refresh            bool   `help:"Bypass cache and fetch from Slack" name:"refresh"`
	Compact            bool   `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
}

type ContextCmd struct {
	Channel            string `help:"Channel ID, #name, or name" name:"channel"`
	TS                 string `help:"Message timestamp" name:"ts"`
	Before             int    `help:"Messages before the target" default:"5" name:"before"`
	After              int    `help:"Messages after the target" default:"5" name:"after"`
	Verbose            bool   `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool   `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Refresh            bool   `help:"Bypass cache and fetch from Slack" name:"refresh"`
	Compact            bool   `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
}

type OpenCmd struct {
	URL                string `arg:"" optional:"" name:"slack-url" help:"Slack archive permalink"`
	Mode               string `help:"Context mode" enum:"message,thread,context" default:"message" name:"mode"`
	Before             int    `help:"Messages before the target for context mode" default:"5" name:"before"`
	After              int    `help:"Messages after the target for context mode" default:"5" name:"after"`
	Verbose            bool   `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool   `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Refresh            bool   `help:"Bypass cache and fetch from Slack" name:"refresh"`
	Compact            bool   `help:"Return compact agent-friendly JSON without rendered text or cache detail" name:"compact"`
}

type HistoryCmd struct {
	Channel            string `help:"Channel ID, #name, or name" name:"channel"`
	Count              int    `help:"Maximum messages; alias: --limit" default:"50" name:"count" aliases:"limit"`
	BeforeTS           string `help:"Fetch messages before this timestamp" name:"before-ts"`
	AfterTS            string `help:"Fetch messages after this timestamp" name:"after-ts"`
	Verbose            bool   `help:"Return full message text" name:"verbose"`
	IncludeRichContent bool   `help:"Include Slack blocks, attachments, and files where available" name:"include-rich-content"`
	Refresh            bool   `help:"Bypass cache and fetch from Slack" name:"refresh"`
}

type ChannelsCmd struct {
	Target  []string `arg:"" optional:"" name:"target" help:"Optional channel ID, #name, or name"`
	Count   int      `help:"Maximum channels; alias: --limit" default:"50" name:"count" aliases:"limit"`
	Refresh bool     `help:"Bypass cache and fetch from Slack" name:"refresh"`
}

type UserCmd struct {
	Target  []string `arg:"" optional:"" name:"target" help:"Email, @handle/name, or user ID"`
	Email   string   `help:"Email address to resolve" name:"email"`
	Refresh bool     `help:"Bypass cache and fetch from Slack" name:"refresh"`
}

type ResolveCmd struct {
	Default ResolveSummaryCmd `cmd:"" default:"noargs" hidden:""`
	User    ResolveUserCmd    `cmd:"" hidden:"" help:"Compatibility alias for slacky user"`
}

type ResolveSummaryCmd struct{}

type ResolveUserCmd struct {
	Target  []string `arg:"" optional:"" name:"target" help:"Email, @handle/name, or user ID"`
	Refresh bool     `help:"Bypass cache and fetch from Slack" name:"refresh"`
}

type searchUserExpansion struct {
	Handle string `json:"handle"`
	UserID string `json:"user_id"`
	Name   string `json:"name,omitempty"`
}

type searchChannelExpansion struct {
	Target    string `json:"target"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name,omitempty"`
}

type resultCommands struct {
	Message     string `json:"message,omitempty"`
	Thread      string `json:"thread,omitempty"`
	Context     string `json:"context,omitempty"`
	RootContext string `json:"root_context,omitempty"`
	Open        string `json:"open,omitempty"`
}

type compactMessageResult struct {
	Ref         int            `json:"ref"`
	ChannelID   string         `json:"channel_id,omitempty"`
	ChannelName string         `json:"channel_name,omitempty"`
	TS          string         `json:"ts,omitempty"`
	ThreadTS    string         `json:"thread_ts,omitempty"`
	RootTS      string         `json:"root_ts,omitempty"`
	Permalink   string         `json:"permalink,omitempty"`
	User        string         `json:"user,omitempty"`
	Username    string         `json:"username,omitempty"`
	DisplayName string         `json:"display_name,omitempty"`
	Datetime    string         `json:"datetime,omitempty"`
	Date        string         `json:"date,omitempty"`
	Excerpt     string         `json:"excerpt,omitempty"`
	Commands    resultCommands `json:"commands"`
}

type compactThreadResult struct {
	Ref          int                    `json:"ref"`
	ChannelID    string                 `json:"channel_id,omitempty"`
	ChannelName  string                 `json:"channel_name,omitempty"`
	RootTS       string                 `json:"root_ts,omitempty"`
	Datetime     string                 `json:"datetime,omitempty"`
	Date         string                 `json:"date,omitempty"`
	Permalink    string                 `json:"permalink,omitempty"`
	MessageCount int                    `json:"message_count"`
	First        compactMessageResult   `json:"first,omitempty"`
	Commands     resultCommands         `json:"commands"`
	Messages     []compactMessageResult `json:"messages,omitempty"`
}

func (cmd *SearchCmd) Run(globals *Globals) error {
	query := strings.Join(cmd.Query, " ")
	if strings.TrimSpace(query) == "" {
		return missingUsage("missing search query", "slacky search <query>", "slacky search \"from:@someone has:link\"", "slacky search --local \"release notes\"")
	}
	pathSet, authStatus, _, err := runtimeState()
	if err != nil {
		return err
	}
	meta := map[string]any{
		"query":                query,
		"count":                cmd.Count,
		"sort":                 cmd.Sort,
		"local":                cmd.Local,
		"verbose":              cmd.Verbose,
		"include_rich_content": cmd.IncludeRichContent,
		"group_by_thread":      cmd.GroupByThread,
		"evidence":             cmd.Evidence,
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Compact: cmd.Compact}
	if !cmd.Local && !authStatus.ReadyForSlack {
		return missingAuthError(pathSet.AuthFile.Path)
	}
	if !cmd.Local {
		var cacheDB *store.DB
		if !globals.NoCache {
			cacheDB, _ = store.Open(pathSet.CacheDB.Path)
			if cacheDB != nil {
				defer func() {
					_ = cacheDB.Close()
				}()
			}
		}
		client, err := slackClient(globals, pathSet, pathSet.AuthFile.Path)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
		defer cancel()
		slackQuery, userExpansions := expandSearchUserHandles(query, func(handle string) (api.UserResult, bool) {
			return resolveSearchUserHandle(ctx, globals, cacheDB, client, handle)
		})
		slackQuery, channelExpansions := expandSearchChannels(slackQuery, func(target string) (api.ChannelResult, bool) {
			return resolveSearchChannel(ctx, globals, cacheDB, client, target)
		})
		if slackQuery != query {
			meta["slack_query"] = slackQuery
		}
		if len(userExpansions) > 0 {
			meta["user_expansions"] = userExpansions
		}
		if len(channelExpansions) > 0 {
			meta["channel_expansions"] = channelExpansions
		}
		result, err := client.SearchMessages(ctx, slackQuery, cmd.Count, cmd.Sort, cmd.IncludeRichContent)
		if err != nil {
			if cacheDB != nil {
				local, localErr := cacheDB.SearchMessages(query, cmd.Count)
				if localErr == nil && len(local.Messages) > 0 {
					displayMessages := messagesForHuman(ctx, globals, cacheDB, client, local.Messages, userExpansions)
					recordRateLimit(globals, err)
					notice := fmt.Sprintf("Slack request failed (%s); using cached local results", err.Error())
					header := []string{
						fmt.Sprintf("Query: %s", query),
						fmt.Sprintf("Showing cached: %d", len(local.Messages)),
						fmt.Sprintf("Notice: %s", notice),
					}
					text := renderSearchMessageOutput(cmd.Evidence, header, displayMessages, query, searchHighlightTerms(query, userExpansions, channelExpansions), renderOptions)
					return writeCompactEnvelope(globals, cmd.Compact, Envelope{
						OK:          true,
						Text:        text,
						Source:      "cache",
						CacheNotice: notice,
						Results:     local.Messages,
						Search:      map[string]any{"meta": meta, "total": len(local.Messages), "suggestions": local.Suggestions},
						Cache:       store.Inspect(pathSet.CacheDB.Path),
					})
				}
			}
			return slackAPIError(err)
		}
		if cacheDB != nil {
			if len(result.Messages) == 0 {
				local, localErr := cacheDB.SearchMessages(query, cmd.Count)
				if localErr == nil && len(local.Messages) > 0 {
					displayMessages := messagesForHuman(ctx, globals, cacheDB, client, local.Messages, userExpansions)
					notice := "Slack returned no matches; using cached local results"
					header := []string{
						fmt.Sprintf("Query: %s", query),
						fmt.Sprintf("Showing cached: %d", len(local.Messages)),
						fmt.Sprintf("Notice: %s", notice),
					}
					text := renderSearchMessageOutput(cmd.Evidence, header, displayMessages, query, searchHighlightTerms(query, userExpansions, channelExpansions), renderOptions)
					return writeCompactEnvelope(globals, cmd.Compact, Envelope{
						OK:          true,
						Text:        text,
						Source:      "slack+cache",
						CacheNotice: notice,
						Results:     local.Messages,
						Search:      map[string]any{"meta": meta, "total": len(local.Messages), "suggestions": local.Suggestions},
						Cache:       store.Inspect(pathSet.CacheDB.Path),
					})
				}
			}
			_ = cacheDB.UpsertMessages(result.Messages)
			_ = cacheDB.UpsertChannels(channelsFromMessages(result.Messages))
			_ = cacheDB.RecordSearch(query, "slack", len(result.Messages))
		}
		header := liveSearchHeader(query, slackQuery, fmt.Sprintf("Showing: %d of %d", len(result.Messages), result.Total))
		text := renderSearchMessageOutput(cmd.Evidence, header, messagesForHuman(ctx, globals, cacheDB, client, result.Messages, userExpansions), query, searchHighlightTerms(query, userExpansions, channelExpansions), renderOptions)
		return writeCompactEnvelope(globals, cmd.Compact, Envelope{
			OK:      true,
			Text:    text,
			Source:  "slack",
			Results: result.Messages,
			Search:  map[string]any{"meta": meta, "total": result.Total, "pagination": result.Pagination},
			Cache:   store.Inspect(pathSet.CacheDB.Path),
		})
	}
	cacheStatus := store.Inspect(pathSet.CacheDB.Path)
	local := store.LocalSearchResult{Messages: []api.MessageResult{}, Suggestions: []string{}}
	if cacheStatus.Exists {
		cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
		if err != nil {
			return err
		}
		defer func() {
			_ = cacheDB.Close()
		}()
		local, err = cacheDB.SearchMessages(query, cmd.Count)
		if err != nil {
			return err
		}
	}
	results := local.Messages
	if results == nil {
		results = []api.MessageResult{}
	}
	notice := ""
	if len(results) == 0 {
		notice = "no cached messages matched"
	} else if len(local.Suggestions) > 0 {
		notice = "returned cached fuzzy suggestions"
	}
	source := "cache"
	header := []string{
		fmt.Sprintf("Query: %s", query),
		fmt.Sprintf("Source: %s", source),
		fmt.Sprintf("Results: %d", len(results)),
	}
	if notice != "" {
		header = append(header, fmt.Sprintf("Notice: %s", notice))
	}
	if len(local.Suggestions) > 0 {
		header = append(header, fmt.Sprintf("Suggestions: %s", strings.Join(local.Suggestions, ", ")))
	}
	if cmd.GroupByThread {
		displayResults := messagesForHuman(context.TODO(), globals, nil, nil, results, nil)
		threads := groupMessagesByThread(displayResults)
		text := renderThreadListWithOptions("Search", header, threads, renderOptions)
		return writeCompactEnvelope(globals, cmd.Compact, Envelope{
			OK:          true,
			Text:        text,
			Source:      source,
			CacheNotice: notice,
			Threads:     groupMessagesByThread(results),
			Search:      map[string]any{"meta": meta, "suggestions": local.Suggestions},
			Cache:       cacheStatus,
		})
	}
	text := renderSearchMessageOutput(cmd.Evidence, header, messagesForHuman(context.TODO(), globals, nil, nil, results, nil), query, searchHighlightTerms(query, nil, nil), renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:          true,
		Text:        text,
		Source:      source,
		CacheNotice: notice,
		Results:     results,
		Search:      map[string]any{"meta": meta, "suggestions": local.Suggestions},
		Cache:       cacheStatus,
	})
}

func (cmd *FindCmd) Run(globals *Globals) error {
	topic := strings.Join(cmd.Topic, " ")
	if strings.TrimSpace(topic) == "" {
		return missingUsage("missing topic", "slacky find <topic>", "slacky find \"release blocker\"", "slacky find \"customer escalation\" --count 5")
	}
	pathSet, authStatus, cacheStatus, err := runtimeState()
	if err != nil {
		return err
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Compact: cmd.Compact}
	local := store.LocalSearchResult{Messages: []api.MessageResult{}, Suggestions: []string{}}
	if !globals.NoCache {
		if cacheDB, cacheErr := store.OpenReadOnly(pathSet.CacheDB.Path); cacheErr == nil {
			local, _ = cacheDB.SearchMessages(topic, cmd.Count)
			_ = cacheDB.Close()
		}
	}
	if !authStatus.ReadyForSlack {
		if len(local.Messages) > 0 {
			displayMessages := messagesForHuman(context.TODO(), globals, nil, nil, local.Messages, nil)
			threads := groupMessagesByThread(displayMessages)
			text := renderThreadListWithOptions("Find", []string{
				fmt.Sprintf("Topic: %s", topic),
				fmt.Sprintf("Ranked cached threads: %d", len(threads)),
			}, threads, renderOptions)
			return writeCompactEnvelope(globals, cmd.Compact, Envelope{
				OK:      true,
				Text:    text,
				Source:  "cache",
				Threads: groupMessagesByThread(local.Messages),
				Search: map[string]any{
					"topic":       topic,
					"count":       cmd.Count,
					"suggestions": local.Suggestions,
				},
				Cache: store.Inspect(pathSet.CacheDB.Path),
			})
		}
		return missingAuthError(pathSet.AuthFile.Path)
	}
	client, err := slackClient(globals, pathSet, pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	searchResult, err := client.SearchMessages(ctx, topic, cmd.Count, "score", cmd.IncludeRichContent)
	if err != nil {
		return slackAPIError(err)
	}
	if !globals.NoCache {
		if cacheDB, cacheErr := store.Open(pathSet.CacheDB.Path); cacheErr == nil {
			_ = cacheDB.UpsertMessages(searchResult.Messages)
			_ = cacheDB.UpsertChannels(channelsFromMessages(searchResult.Messages))
			_ = cacheDB.RecordSearch(topic, "slack", len(searchResult.Messages))
			_ = cacheDB.Close()
		}
	}
	combined := mergeMessages(searchResult.Messages, local.Messages)
	displayCombined := messagesForHuman(ctx, globals, nil, client, combined, nil)
	threads := groupMessagesByThread(displayCombined)
	source := "slack"
	notice := ""
	if len(local.Messages) > 0 {
		source = "slack+cache"
		notice = "included cached FTS/fuzzy matches"
	}
	header := []string{
		fmt.Sprintf("Topic: %s", topic),
		fmt.Sprintf("Ranked threads: %d", len(threads)),
	}
	if notice != "" {
		header = append(header, fmt.Sprintf("Notice: %s", notice))
	}
	text := renderThreadListWithOptions("Find", header, threads, renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:          true,
		Text:        text,
		Source:      source,
		CacheNotice: notice,
		Threads:     groupMessagesByThread(combined),
		Search: map[string]any{
			"topic":                topic,
			"count":                cmd.Count,
			"verbose":              cmd.Verbose,
			"include_rich_content": cmd.IncludeRichContent,
			"suggestions":          local.Suggestions,
		},
		Cache: cacheStatus,
	})
}

func (cmd *MessageCmd) Run(globals *Globals) error {
	if cmd.Channel == "" {
		return missingUsage("missing channel", "slacky message --channel <id|name> --ts <ts>", "slacky channels", "slacky message --channel C123 --ts 1717440000.000000")
	}
	if cmd.TS == "" {
		return missingUsage("missing timestamp", "slacky message --channel <id|name> --ts <ts>", "slacky search \"release notes\"", "slacky message --channel C123 --ts 1717440000.000000")
	}
	client, err := liveWorkflowClient(globals)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	channelID, err := resolveChannelID(ctx, globals, client, cmd.Channel, cmd.Refresh)
	if err != nil {
		return err
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent}
	message, err := client.Message(ctx, channelID, cmd.TS, cmd.IncludeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if cached, found := cachedMessage(channelID, cmd.TS); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "message")
				return writeCachedMessages(globals, ctx, client, "Message", nil, []api.MessageResult{cached}, notice, renderOptions, api.ThreadResult{})
			}
		}
		if isSlackMessageNotFound(err) {
			if cached, found := cachedMessage(channelID, cmd.TS); found {
				notice := "Slack history did not return this timestamp; using cached message"
				return writeCachedMessages(globals, ctx, client, "Message", nil, []api.MessageResult{cached}, notice, renderOptions, api.ThreadResult{})
			}
		}
		return slackAPIError(err)
	}
	cacheStatus := cacheMessagesWithState(globals, []api.MessageResult{message}, store.FetchState{
		Kind:           "message",
		Scope:          fetchScope(channelID, cmd.TS),
		CursorOrWindow: "exact",
		Value: map[string]any{
			"channel_id": channelID,
			"ts":         cmd.TS,
			"permalink":  message.Permalink,
		},
	})
	displayMessages := messagesForHuman(ctx, globals, nil, client, []api.MessageResult{message}, nil)
	displayMessage := message
	if len(displayMessages) > 0 {
		displayMessage = displayMessages[0]
	}
	text := renderMessageListWithOptions("Message", nil, displayMessages, renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:      true,
		Text:    text,
		Source:  "slack",
		Message: displayMessage,
		Results: []api.MessageResult{displayMessage},
		Cache:   cacheStatus,
	})
}

func (cmd *ThreadCmd) Run(globals *Globals) error {
	if cmd.Channel == "" {
		return missingUsage("missing channel", "slacky thread --channel <id|name> --ts <parent-or-reply-ts>", "slacky channels", "slacky thread --channel C123 --ts 1717440000.000000")
	}
	if cmd.TS == "" {
		return missingUsage("missing timestamp", "slacky thread --channel <id|name> --ts <parent-or-reply-ts>", "slacky search \"release notes\"", "slacky thread --channel C123 --ts 1717440000.000000")
	}
	client, err := liveWorkflowClient(globals)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	channelID, err := resolveChannelID(ctx, globals, client, cmd.Channel, cmd.Refresh)
	if err != nil {
		return err
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Compact: cmd.Compact}
	rootTS := ""
	if !globals.NoCache {
		rootTS = resolveCachedThreadRootTS(channelID, cmd.TS)
	}
	if rootTS == "" {
		rootTS = cmd.TS
		if message, messageErr := client.MessageAt(ctx, channelID, cmd.TS, cmd.IncludeRichContent); messageErr == nil {
			cacheMessagesWithState(globals, []api.MessageResult{message}, store.FetchState{
				Kind:           "message",
				Scope:          fetchScope(channelID, cmd.TS),
				CursorOrWindow: "thread-root-resolution",
				Value: map[string]any{
					"channel_id": channelID,
					"ts":         cmd.TS,
					"root_ts":    messageRootTS(message),
				},
			})
			rootTS = firstNonEmpty(messageRootTS(message), cmd.TS)
		} else if isRateLimitError(messageErr) {
			recordRateLimit(globals, messageErr)
			if !globals.NoCache {
				if thread, found := cachedThread(channelID, rootTS); found {
					notice := rateLimitCacheNotice(messageErr, "thread")
					return writeCachedThread(globals, ctx, client, "Thread", nil, thread, notice, renderOptions)
				}
			}
		}
	}
	thread, err := client.Thread(ctx, channelID, rootTS, cmd.IncludeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if thread, found := cachedThread(channelID, rootTS); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "thread")
				return writeCachedThread(globals, ctx, client, "Thread", nil, thread, notice, renderOptions)
			}
		}
		return slackAPIError(err)
	}
	cacheStatus := cacheThread(globals, thread)
	displayThread := threadForHuman(ctx, globals, client, thread)
	text := renderThreadListWithOptions("Thread", nil, []api.ThreadResult{displayThread}, renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:      true,
		Text:    text,
		Source:  "slack",
		Thread:  displayThread,
		Results: displayThread.Messages,
		Cache:   cacheStatus,
	})
}

func (cmd *ContextCmd) Run(globals *Globals) error {
	if cmd.Channel == "" {
		return missingUsage("missing channel", "slacky context --channel <id|name> --ts <ts>", "slacky channels", "slacky context --channel C123 --ts 1717440000.000000")
	}
	if cmd.TS == "" {
		return missingUsage("missing timestamp", "slacky context --channel <id|name> --ts <ts>", "slacky search \"release notes\"", "slacky context --channel C123 --ts 1717440000.000000")
	}
	client, err := liveWorkflowClient(globals)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	channelID, err := resolveChannelID(ctx, globals, client, cmd.Channel, cmd.Refresh)
	if err != nil {
		return err
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Compact: cmd.Compact}
	if !globals.NoCache {
		if root := resolveCachedThreadRootTS(channelID, cmd.TS); root != "" && root != cmd.TS {
			return writeThreadReplyContext(globals, ctx, client, channelID, cmd.TS, root, cmd.IncludeRichContent, renderOptions)
		}
	}
	beforeMessages := []api.MessageResult{}
	if cmd.Before > 0 {
		beforeMessages, err = client.ConversationHistory(ctx, channelID, cmd.Before, cmd.TS, "", cmd.IncludeRichContent)
		if err != nil {
			if isRateLimitError(err) && !globals.NoCache {
				if messages, found := cachedContextMessages(channelID, cmd.TS, cmd.Before, cmd.After); found {
					recordRateLimit(globals, err)
					notice := rateLimitCacheNotice(err, "context")
					return writeCachedMessages(globals, ctx, client, "Context", []string{
						fmt.Sprintf("Channel: %s", channelID),
						fmt.Sprintf("TS: %s", cmd.TS),
					}, messages, notice, renderOptions, api.ThreadResult{})
				}
			}
			return slackAPIError(err)
		}
	}
	target, err := client.Message(ctx, channelID, cmd.TS, cmd.IncludeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if messages, found := cachedContextMessages(channelID, cmd.TS, cmd.Before, cmd.After); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "context")
				return writeCachedMessages(globals, ctx, client, "Context", []string{
					fmt.Sprintf("Channel: %s", channelID),
					fmt.Sprintf("TS: %s", cmd.TS),
				}, messages, notice, renderOptions, api.ThreadResult{})
			}
		}
		if isSlackMessageNotFound(err) {
			if cached, found := cachedMessage(channelID, cmd.TS); found {
				if root := messageRootTS(cached); root != "" && root != cmd.TS {
					thread, threadErr := client.Thread(ctx, channelID, root, cmd.IncludeRichContent)
					if threadErr != nil {
						return slackAPIError(threadErr)
					}
					cacheStatus := cacheThread(globals, thread)
					notice := "target timestamp is a thread reply; showing the containing thread"
					displayThread := threadForHuman(ctx, globals, client, thread)
					text := renderMessageListWithOptions("Context", []string{
						fmt.Sprintf("Channel: %s", channelID),
						fmt.Sprintf("TS: %s", cmd.TS),
						fmt.Sprintf("Root: %s", root),
						fmt.Sprintf("Notice: %s", notice),
					}, displayThread.Messages, renderOptions)
					return writeCompactEnvelope(globals, cmd.Compact, Envelope{
						OK:          true,
						Text:        text,
						Source:      "slack",
						CacheNotice: notice,
						Results:     displayThread.Messages,
						Thread:      displayThread,
						Cache:       cacheStatus,
					})
				}
			}
		}
		return slackAPIError(err)
	}
	afterMessages := []api.MessageResult{}
	if cmd.After > 0 {
		afterMessages, err = client.ConversationHistory(ctx, channelID, cmd.After, "", cmd.TS, cmd.IncludeRichContent)
		if err != nil {
			if isRateLimitError(err) && !globals.NoCache {
				if messages, found := cachedContextMessages(channelID, cmd.TS, cmd.Before, cmd.After); found {
					recordRateLimit(globals, err)
					notice := rateLimitCacheNotice(err, "context")
					return writeCachedMessages(globals, ctx, client, "Context", []string{
						fmt.Sprintf("Channel: %s", channelID),
						fmt.Sprintf("TS: %s", cmd.TS),
					}, messages, notice, renderOptions, api.ThreadResult{})
				}
			}
			return slackAPIError(err)
		}
	}
	messages := append(reverseMessages(beforeMessages), target)
	messages = append(messages, reverseMessages(afterMessages)...)
	cacheStatus := cacheMessagesWithState(globals, messages, store.FetchState{
		Kind:           "context_window",
		Scope:          fetchScope(channelID, cmd.TS),
		CursorOrWindow: fmt.Sprintf("before:%d after:%d", cmd.Before, cmd.After),
		Value: map[string]any{
			"channel_id": channelID,
			"ts":         cmd.TS,
			"before":     cmd.Before,
			"after":      cmd.After,
			"returned":   len(messages),
		},
	})
	displayMessages := messagesForHuman(ctx, globals, nil, client, messages, nil)
	text := renderMessageListWithOptions("Context", []string{
		fmt.Sprintf("Channel: %s", channelID),
		fmt.Sprintf("TS: %s", cmd.TS),
		fmt.Sprintf("Before: %d", cmd.Before),
		fmt.Sprintf("After: %d", cmd.After),
	}, displayMessages, renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:      true,
		Text:    text,
		Source:  "slack",
		Results: displayMessages,
		Cache:   cacheStatus,
	})
}

func writeThreadReplyContext(globals *Globals, ctx context.Context, client *api.Client, channelID string, targetTS string, rootTS string, includeRichContent bool, renderOptions messageRenderOptions) error {
	thread, err := client.Thread(ctx, channelID, rootTS, includeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if cached, found := cachedThread(channelID, rootTS); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "thread context")
				return writeCachedMessages(globals, ctx, client, "Context", []string{
					fmt.Sprintf("Channel: %s", channelID),
					fmt.Sprintf("TS: %s", targetTS),
					fmt.Sprintf("Root: %s", rootTS),
				}, cached.Messages, notice, renderOptions, cached)
			}
		}
		return slackAPIError(err)
	}
	cacheStatus := cacheThread(globals, thread)
	notice := "target timestamp is a thread reply; showing the containing thread"
	displayThread := threadForHuman(ctx, globals, client, thread)
	text := renderMessageListWithOptions("Context", []string{
		fmt.Sprintf("Channel: %s", channelID),
		fmt.Sprintf("TS: %s", targetTS),
		fmt.Sprintf("Root: %s", rootTS),
		fmt.Sprintf("Notice: %s", notice),
	}, displayThread.Messages, renderOptions)
	return writeCompactEnvelope(globals, renderOptions.Compact, Envelope{
		OK:          true,
		Text:        text,
		Source:      "slack",
		CacheNotice: notice,
		Results:     displayThread.Messages,
		Thread:      displayThread,
		Cache:       cacheStatus,
	})
}

func (cmd *OpenCmd) Run(globals *Globals) error {
	if strings.TrimSpace(cmd.URL) == "" {
		return missingUsage("missing Slack archive URL", "slacky open <slack-url>", "slacky open https://workspace.slack.com/archives/C123/p1717440000000000", "slacky open <url> --mode thread")
	}
	ref, err := resolve.ParseArchiveURL(cmd.URL)
	if err != nil {
		return appError("invalid_slack_url", err.Error())
	}
	switch cmd.Mode {
	case "thread":
		thread := ThreadCmd{Channel: ref.ChannelID, TS: firstNonEmpty(ref.ThreadTS, ref.TS), Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Refresh: cmd.Refresh, Compact: cmd.Compact}
		return thread.Run(globals)
	case "context":
		contextCmd := ContextCmd{Channel: ref.ChannelID, TS: firstNonEmpty(ref.ThreadTS, ref.TS), Before: cmd.Before, After: cmd.After, Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Refresh: cmd.Refresh, Compact: cmd.Compact}
		return contextCmd.Run(globals)
	default:
		if ref.ThreadTS != "" && ref.ThreadTS != ref.TS {
			return renderMessageFromThread(globals, ref.ChannelID, ref.ThreadTS, ref.TS, cmd.Verbose, cmd.IncludeRichContent, cmd.Compact)
		}
		message := MessageCmd{Channel: ref.ChannelID, TS: ref.TS, Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent, Refresh: cmd.Refresh, Compact: cmd.Compact}
		return message.Run(globals)
	}
}

func (cmd *HistoryCmd) Run(globals *Globals) error {
	if cmd.Channel == "" {
		return missingUsage("missing channel", "slacky history --channel <id|name>", "slacky channels", "slacky history --channel general --count 25")
	}
	client, err := liveWorkflowClient(globals)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	channelID, err := resolveChannelID(ctx, globals, client, cmd.Channel, cmd.Refresh)
	if err != nil {
		return err
	}
	renderOptions := messageRenderOptions{Verbose: cmd.Verbose, IncludeRichContent: cmd.IncludeRichContent}
	messages, err := client.ConversationHistory(ctx, channelID, cmd.Count, cmd.BeforeTS, cmd.AfterTS, cmd.IncludeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if messages, found := cachedChannelMessages(channelID, cmd.Count); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "history")
				return writeCachedMessages(globals, ctx, client, "History", []string{
					fmt.Sprintf("Channel: %s", channelID),
					fmt.Sprintf("Messages: %d", len(messages)),
				}, messages, notice, renderOptions, api.ThreadResult{})
			}
		}
		return slackAPIError(err)
	}
	cacheStatus := cacheMessagesWithState(globals, messages, store.FetchState{
		Kind:           "channel_history",
		Scope:          channelID,
		CursorOrWindow: historyWindow(cmd.BeforeTS, cmd.AfterTS),
		Value: map[string]any{
			"channel_id": channelID,
			"before_ts":  cmd.BeforeTS,
			"after_ts":   cmd.AfterTS,
			"requested":  cmd.Count,
			"returned":   len(messages),
		},
	})
	text := renderMessageListWithOptions("History", []string{
		fmt.Sprintf("Channel: %s", channelID),
		fmt.Sprintf("Messages: %d", len(messages)),
	}, messagesForHuman(ctx, globals, nil, client, messages, nil), renderOptions)
	return writeEnvelope(globals, Envelope{
		OK:      true,
		Text:    text,
		Source:  "slack",
		Results: messages,
		Cache:   cacheStatus,
	})
}

func (cmd *ChannelsCmd) Run(globals *Globals) error {
	pathSet, authStatus, cacheStatus, err := runtimeState()
	if err != nil {
		return err
	}
	target := strings.Join(cmd.Target, " ")
	if !cmd.Refresh && !globals.NoCache {
		if cacheDB, cacheErr := store.OpenReadOnly(pathSet.CacheDB.Path); cacheErr == nil {
			channels, channelsErr := cacheDB.Channels(target, cmd.Count)
			_ = cacheDB.Close()
			if channelsErr == nil && len(channels) > 0 {
				text := renderChannelList(target, channels)
				return writeEnvelope(globals, Envelope{
					OK:       true,
					Text:     text,
					Source:   "cache",
					Channels: channels,
					Cache:    store.Inspect(pathSet.CacheDB.Path),
				})
			}
		}
	}
	if !authStatus.ReadyForSlack {
		return missingAuthError(pathSet.AuthFile.Path)
	}
	client, err := slackClient(globals, pathSet, pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	channelFetchLimit := cmd.Count
	if target != "" {
		channelFetchLimit = 0
	}
	channels, err := client.ConversationsList(ctx, channelFetchLimit)
	if err != nil {
		return slackAPIError(err)
	}
	if target != "" {
		channels = filterChannels(channels, target)
	}
	if !globals.NoCache {
		if cacheDB, cacheErr := store.Open(pathSet.CacheDB.Path); cacheErr == nil {
			_ = cacheDB.UpsertChannels(channels)
			_ = cacheDB.Close()
			cacheStatus = store.Inspect(pathSet.CacheDB.Path)
		}
	}
	text := renderChannelList(target, channels)
	return writeEnvelope(globals, Envelope{
		OK:       true,
		Text:     text,
		Source:   "slack",
		Channels: channels,
		Cache:    cacheStatus,
	})
}

func (cmd *UserCmd) Run(globals *Globals) error {
	target := strings.Join(cmd.Target, " ")
	if cmd.Email != "" {
		target = cmd.Email
	}
	if strings.TrimSpace(target) == "" {
		return missingUsage("missing user", "slacky user <email|@handle|user-id>", "slacky user person@example.com", "slacky user @sampleuser", "slacky user U123")
	}
	return resolveUser(globals, target, cmd.Refresh)
}

func (cmd *ResolveSummaryCmd) Run(globals *Globals) error {
	text := strings.Join([]string{
		"Resolve",
		"",
		"Preferred:",
		"  slacky user <email|@handle|user-id>",
		"",
		"Compatibility:",
		"  slacky resolve user <email|@handle|user-id>",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:       true,
		Text:     text,
		Commands: []string{"slacky user <email|@handle|user-id>", "slacky resolve user <email|@handle|user-id>"},
	})
}

func (cmd *ResolveUserCmd) Run(globals *Globals) error {
	target := strings.Join(cmd.Target, " ")
	if strings.TrimSpace(target) == "" {
		return missingUsage("missing user", "slacky user <email|@handle|user-id>", "slacky user person@example.com", "slacky user @sampleuser", "slacky user U123")
	}
	return resolveUser(globals, target, cmd.Refresh)
}

func resolveUser(globals *Globals, target string, refresh bool) error {
	target = strings.TrimSpace(target)
	pathSet, authStatus, cacheStatus, err := runtimeState()
	if err != nil {
		return err
	}
	if !refresh && !globals.NoCache {
		if cacheDB, cacheErr := store.OpenReadOnly(pathSet.CacheDB.Path); cacheErr == nil {
			user, found, userErr := cachedUserByTarget(cacheDB, target)
			_ = cacheDB.Close()
			if userErr == nil && found {
				return renderUser(globals, user, "cache", cacheStatus)
			}
		}
	}
	if !authStatus.ReadyForSlack {
		return missingAuthError(pathSet.AuthFile.Path)
	}
	client, err := slackClient(globals, pathSet, pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	user, err := liveUserByTarget(ctx, globals, client, target)
	if err != nil {
		return slackAPIError(err)
	}
	if !globals.NoCache {
		if cacheDB, cacheErr := store.Open(pathSet.CacheDB.Path); cacheErr == nil {
			_ = cacheDB.UpsertUser(user)
			_ = cacheDB.Close()
			cacheStatus = store.Inspect(pathSet.CacheDB.Path)
		}
	}
	return renderUser(globals, user, "slack", cacheStatus)
}

func cachedUserByTarget(cacheDB *store.DB, target string) (api.UserResult, bool, error) {
	if isEmailTarget(target) {
		return cacheDB.UserByEmail(target)
	}
	return cacheDB.UserByName(target)
}

func liveUserByTarget(ctx context.Context, globals *Globals, client *api.Client, target string) (api.UserResult, error) {
	cleanTarget := strings.TrimPrefix(strings.TrimSpace(target), "@")
	if isEmailTarget(target) {
		return client.LookupUserByEmail(ctx, target)
	}
	if looksUserID(cleanTarget) {
		return client.UserInfo(ctx, cleanTarget)
	}
	user, suggestions, ok := resolveSearchUserHandleWithSuggestions(ctx, globals, nil, client, cleanTarget)
	if !ok {
		return api.UserResult{}, userNotFoundError(target, suggestions)
	}
	return user, nil
}

func userNotFoundError(target string, suggestions []api.UserResult) *AppError {
	err := appError("user_not_found", fmt.Sprintf("could not resolve user %q", target))
	err.Examples = []string{"slacky user person@example.com", "slacky user @someone", "slacky search \"@someone\" --json"}
	err.Suggestions = userSuggestionLabels(suggestions)
	err.SuggestedCommands = userSuggestionCommands(suggestions)
	if len(err.SuggestedCommands) == 0 {
		err.SuggestedCommands = []string{"slacky user person@example.com --json", "slacky search \"@someone\" --json"}
	}
	return err
}

func isEmailTarget(target string) bool {
	target = strings.TrimSpace(target)
	return strings.Contains(target, "@") && strings.Contains(target, ".") && !strings.HasPrefix(target, "@")
}

func renderUser(globals *Globals, user api.UserResult, source string, cacheStatus store.Status) error {
	text := strings.Join([]string{
		"User",
		"",
		fmt.Sprintf("ID: %s", user.ID),
		fmt.Sprintf("Name: %s", user.Name),
		fmt.Sprintf("Real name: %s", user.RealName),
		fmt.Sprintf("Email: %s", user.Email),
		"",
		"Next:",
		"  slacky search \"from:@" + userSearchLabel(user) + "\"",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   text,
		Source: source,
		User:   user,
		Cache:  cacheStatus,
	})
}

func runtimeState() (paths.Set, config.AuthStatus, store.Status, error) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return paths.Set{}, config.AuthStatus{}, store.Status{}, err
	}
	return pathSet, inspectAuthStatus(pathSet), store.Inspect(pathSet.CacheDB.Path), nil
}

func currentCacheStatus() store.Status {
	pathSet, err := paths.Resolve()
	if err != nil {
		return store.Status{Error: err.Error()}
	}
	return store.Inspect(pathSet.CacheDB.Path)
}

func cachedMessage(channelID string, ts string) (api.MessageResult, bool) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return api.MessageResult{}, false
	}
	cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
	if err != nil {
		return api.MessageResult{}, false
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	message, found, err := cacheDB.Message(channelID, ts)
	if err != nil || !found {
		return api.MessageResult{}, false
	}
	return message, true
}

func cachedThread(channelID string, rootTS string) (api.ThreadResult, bool) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return api.ThreadResult{}, false
	}
	cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
	if err != nil {
		return api.ThreadResult{}, false
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	thread, found, err := cacheDB.Thread(channelID, rootTS)
	if err != nil || !found {
		return api.ThreadResult{}, false
	}
	return thread, true
}

func cachedChannelMessages(channelID string, count int) ([]api.MessageResult, bool) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return nil, false
	}
	cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
	if err != nil {
		return nil, false
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	messages, err := cacheDB.ChannelMessages(channelID, count)
	if err != nil || len(messages) == 0 {
		return nil, false
	}
	return messages, true
}

func cachedContextMessages(channelID string, ts string, before int, after int) ([]api.MessageResult, bool) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return nil, false
	}
	cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
	if err != nil {
		return nil, false
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	messages, err := cacheDB.ContextMessages(channelID, ts, before, after)
	if err != nil || len(messages) == 0 {
		return nil, false
	}
	return messages, true
}

func writeCompactEnvelope(globals *Globals, compact bool, envelope Envelope) error {
	if compact && globals.JSON {
		envelope.Text = ""
		envelope.Cache = nil
		hasCompactMessage := false
		if message, ok := envelope.Message.(api.MessageResult); ok {
			envelope.Message = compactMessageResultFor(1, message)
			envelope.Results = nil
			hasCompactMessage = true
		}
		if thread, ok := envelope.Thread.(api.ThreadResult); ok {
			envelope.Thread = compactThreadResultFor(1, thread, !hasCompactMessage)
			envelope.Results = nil
		}
		if messages, ok := envelope.Results.([]api.MessageResult); ok {
			envelope.Results = compactMessageResults(messages)
		}
		if threads, ok := envelope.Threads.([]api.ThreadResult); ok {
			envelope.Threads = compactThreadResults(threads)
		}
	}
	return writeEnvelope(globals, envelope)
}

func writeCachedMessages(globals *Globals, ctx context.Context, client *api.Client, title string, header []string, messages []api.MessageResult, notice string, options messageRenderOptions, thread api.ThreadResult) error {
	displayMessages := messagesForHuman(ctx, globals, nil, client, messages, nil)
	text := renderMessageListWithOptions(title, noticeHeader(header, notice), displayMessages, options)
	envelope := Envelope{
		OK:          true,
		Text:        text,
		Source:      "cache",
		CacheNotice: notice,
		Results:     displayMessages,
		Cache:       currentCacheStatus(),
	}
	if title == "Message" && len(messages) == 1 {
		envelope.Message = displayMessages[0]
	}
	if thread.ChannelID != "" || len(thread.Messages) > 0 {
		thread.Messages = displayMessages
		envelope.Thread = thread
	}
	return writeCompactEnvelope(globals, options.Compact, envelope)
}

func writeCachedThread(globals *Globals, ctx context.Context, client *api.Client, title string, header []string, thread api.ThreadResult, notice string, options messageRenderOptions) error {
	displayThread := threadForHuman(ctx, globals, client, thread)
	text := renderThreadListWithOptions(title, noticeHeader(header, notice), []api.ThreadResult{displayThread}, options)
	return writeCompactEnvelope(globals, options.Compact, Envelope{
		OK:          true,
		Text:        text,
		Source:      "cache",
		CacheNotice: notice,
		Thread:      displayThread,
		Results:     displayThread.Messages,
		Cache:       currentCacheStatus(),
	})
}

func noticeHeader(header []string, notice string) []string {
	if notice == "" {
		return header
	}
	next := append([]string{}, header...)
	return append(next, fmt.Sprintf("Notice: %s", notice))
}

func isSlackMessageNotFound(err error) bool {
	var slackErr api.SlackError
	return errors.As(err, &slackErr) && slackErr.Code == "message_not_found"
}

func isRateLimitError(err error) bool {
	var rateLimit api.RateLimitError
	return errors.As(err, &rateLimit)
}

func rateLimitCacheNotice(err error, fallback string) string {
	return fmt.Sprintf("Slack rate limited (%s); using cached %s", err.Error(), fallback)
}

func resolveCachedThreadRootTS(channelID string, ts string) string {
	if cached, found := cachedMessage(channelID, ts); found {
		return firstNonEmpty(messageRootTS(cached), ts)
	}
	return ""
}

func renderMessageFromThread(globals *Globals, channelID string, rootTS string, messageTS string, verbose bool, includeRichContent bool, compact bool) error {
	client, err := liveWorkflowClient(globals)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	renderOptions := messageRenderOptions{Verbose: verbose, IncludeRichContent: includeRichContent, Compact: compact}
	thread, err := client.Thread(ctx, channelID, rootTS, includeRichContent)
	if err != nil {
		if isRateLimitError(err) && !globals.NoCache {
			if thread, found := cachedThread(channelID, rootTS); found {
				recordRateLimit(globals, err)
				notice := rateLimitCacheNotice(err, "thread")
				for index := range thread.Messages {
					message := thread.Messages[index]
					if message.TS == messageTS {
						return writeCachedMessages(globals, ctx, client, "Message", []string{
							fmt.Sprintf("Thread: %s", rootTS),
						}, []api.MessageResult{message}, notice, renderOptions, thread)
					}
				}
			}
		}
		return slackAPIError(err)
	}
	cacheStatus := cacheThread(globals, thread)
	for index := range thread.Messages {
		message := thread.Messages[index]
		if message.TS == messageTS {
			displayThread := threadForHuman(ctx, globals, client, thread)
			displayMessage := message
			for displayIndex := range displayThread.Messages {
				if displayThread.Messages[displayIndex].TS == messageTS {
					displayMessage = displayThread.Messages[displayIndex]
					break
				}
			}
			text := renderMessageListWithOptions("Message", []string{
				fmt.Sprintf("Thread: %s", rootTS),
			}, []api.MessageResult{displayMessage}, renderOptions)
			return writeCompactEnvelope(globals, compact, Envelope{
				OK:      true,
				Text:    text,
				Source:  "slack",
				Message: displayMessage,
				Results: []api.MessageResult{displayMessage},
				Thread:  displayThread,
				Cache:   cacheStatus,
			})
		}
	}
	err = appError("message_not_found", fmt.Sprintf("thread %s did not contain message %s", rootTS, messageTS))
	return err
}

func cacheMessagesWithState(globals *Globals, messages []api.MessageResult, state store.FetchState) store.Status {
	pathSet, err := paths.Resolve()
	if err != nil {
		return store.Status{Error: err.Error()}
	}
	if globals.NoCache {
		return store.Inspect(pathSet.CacheDB.Path)
	}
	cacheDB, err := store.Open(pathSet.CacheDB.Path)
	if err != nil {
		status := store.Inspect(pathSet.CacheDB.Path)
		status.Error = err.Error()
		return status
	}
	if err := cacheDB.UpsertMessages(messages); err != nil {
		_ = cacheDB.Close()
		status := store.Inspect(pathSet.CacheDB.Path)
		status.Error = err.Error()
		return status
	}
	_ = cacheDB.UpsertChannels(channelsFromMessages(messages))
	if state.Kind != "" {
		_ = cacheDB.RecordFetchState(state)
	}
	_ = cacheDB.Close()
	return store.Inspect(pathSet.CacheDB.Path)
}

func cacheThread(globals *Globals, thread api.ThreadResult) store.Status {
	pathSet, err := paths.Resolve()
	if err != nil {
		return store.Status{Error: err.Error()}
	}
	if globals.NoCache {
		return store.Inspect(pathSet.CacheDB.Path)
	}
	cacheDB, err := store.Open(pathSet.CacheDB.Path)
	if err != nil {
		status := store.Inspect(pathSet.CacheDB.Path)
		status.Error = err.Error()
		return status
	}
	if err := cacheDB.UpsertThread(thread); err != nil {
		_ = cacheDB.Close()
		status := store.Inspect(pathSet.CacheDB.Path)
		status.Error = err.Error()
		return status
	}
	_ = cacheDB.RecordFetchState(store.FetchState{
		Kind:           "thread",
		Scope:          fetchScope(thread.ChannelID, thread.RootTS),
		CursorOrWindow: "complete",
		Value: map[string]any{
			"channel_id":    thread.ChannelID,
			"root_ts":       thread.RootTS,
			"message_count": len(thread.Messages),
			"permalink":     thread.Permalink,
		},
	})
	_ = cacheDB.Close()
	return store.Inspect(pathSet.CacheDB.Path)
}

func fetchScope(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "|")
}

func historyWindow(beforeTS string, afterTS string) string {
	switch {
	case beforeTS != "" && afterTS != "":
		return "after:" + afterTS + " before:" + beforeTS
	case beforeTS != "":
		return "before:" + beforeTS
	case afterTS != "":
		return "after:" + afterTS
	default:
		return "latest"
	}
}

func liveWorkflowClient(globals *Globals) (*api.Client, error) {
	pathSet, authStatus, _, err := runtimeState()
	if err != nil {
		return nil, err
	}
	if !authStatus.ReadyForSlack {
		return nil, missingAuthError(pathSet.AuthFile.Path)
	}
	client, err := slackClient(globals, pathSet, pathSet.AuthFile.Path)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func slackClient(globals *Globals, pathSet paths.Set, authPath string) (*api.Client, error) {
	var auth config.Auth
	var err error
	if authPath == pathSet.AuthFile.Path {
		auth, err = refreshActiveAuthIfDue(globals, pathSet)
	} else {
		auth, err = config.LoadAuth(authPath)
	}
	if err != nil {
		return nil, err
	}
	client := apiClientFromAuth(globals, auth)
	client.Cooldown = activeCooldown
	return client, nil
}

func resolveChannelID(ctx context.Context, globals *Globals, client *api.Client, target string, refresh bool) (string, error) {
	if channelID, ok := channelIDFromTarget(target); ok {
		if refresh && !globals.NoCache {
			if channel, err := client.ConversationInfo(ctx, channelID); err == nil {
				cacheChannels(globals, []api.ChannelResult{channel})
			}
		}
		return channelID, nil
	}
	pathSet, err := paths.Resolve()
	if err != nil {
		return "", err
	}
	if !refresh && !globals.NoCache {
		if cacheDB, cacheErr := store.OpenReadOnly(pathSet.CacheDB.Path); cacheErr == nil {
			channels, channelsErr := cacheDB.Channels(target, 1)
			_ = cacheDB.Close()
			if channelsErr == nil && len(channels) > 0 {
				return channels[0].ID, nil
			}
		}
	}
	channels, err := client.ConversationsList(ctx, 0)
	if err != nil {
		return "", slackAPIError(err)
	}
	cacheChannels(globals, channels)
	filtered := filterChannels(channels, target)
	if len(filtered) == 0 {
		err := appError("channel_not_found", fmt.Sprintf("could not resolve channel %q", target))
		err.Examples = []string{"slacky channels", "slacky channels --refresh", "slacky history --channel C123"}
		return "", err
	}
	return filtered[0].ID, nil
}

func channelIDFromTarget(target string) (string, bool) {
	target = strings.TrimSpace(target)
	if matches := slackChannelMentionPattern.FindStringSubmatch(target); len(matches) >= 2 {
		return matches[1], true
	}
	if strings.HasPrefix(target, "#") {
		return "", false
	}
	if len(target) < 2 {
		return "", false
	}
	switch target[0] {
	case 'C', 'G', 'D':
		return target, true
	default:
		return "", false
	}
}

func cacheChannels(globals *Globals, channels []api.ChannelResult) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return
	}
	if globals.NoCache {
		return
	}
	cacheDB, err := store.Open(pathSet.CacheDB.Path)
	if err != nil {
		return
	}
	_ = cacheDB.UpsertChannels(channels)
	_ = cacheDB.Close()
}

func activeCooldown(method string) (api.RateLimitError, bool) {
	pathSet, err := paths.Resolve()
	if err != nil {
		return api.RateLimitError{}, false
	}
	cacheDB, err := store.OpenReadOnly(pathSet.CacheDB.Path)
	if err != nil {
		return api.RateLimitError{}, false
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	cooldown, ok := cacheDB.ActiveCooldown(method)
	if !ok {
		return api.RateLimitError{}, false
	}
	retryAt, err := time.Parse(time.RFC3339, cooldown.RetryAt)
	if err != nil {
		return api.RateLimitError{}, false
	}
	retryAfter := time.Until(retryAt)
	if retryAfter <= 0 {
		return api.RateLimitError{}, false
	}
	return api.RateLimitError{
		Method:     method,
		RetryAfter: retryAfter.Round(time.Second),
		RetryAt:    retryAt,
	}, true
}

func slackAPIError(err error) error {
	recordRateLimit(nil, err)
	var rateLimit api.RateLimitError
	if errors.As(err, &rateLimit) {
		appErr := appError("rate_limited", rateLimit.Error())
		appErr.SuggestedCommands = []string{"slacky cache status", "slacky search --local <query>"}
		return appErr
	}
	var slackErr api.SlackError
	if errors.As(err, &slackErr) {
		appErr := appError("slack_api_error", slackErr.Error())
		appErr.SuggestedCommands = []string{"slacky auth status", "slacky doctor"}
		return appErr
	}
	return err
}

func recordRateLimit(globals *Globals, err error) {
	var rateLimit api.RateLimitError
	if !errors.As(err, &rateLimit) {
		return
	}
	pathSet, pathErr := paths.Resolve()
	if pathErr != nil {
		return
	}
	cacheDB, cacheErr := store.Open(pathSet.CacheDB.Path)
	if cacheErr != nil {
		return
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	source := "slack"
	if globals != nil && globals.NoCache {
		source = "slack-no-cache"
	}
	_ = cacheDB.RecordCooldown(rateLimit.Method, rateLimit.RetryAfter, rateLimit.RetryAt, source)
}

func expandSearchUserHandles(query string, resolve func(handle string) (api.UserResult, bool)) (string, []searchUserExpansion) {
	resolved := map[string]api.UserResult{}
	expansions := []searchUserExpansion{}
	expand := func(prefix string, operator string, handle string, original string) string {
		if searchAlreadyExpandedUserID.MatchString(handle) {
			return original
		}
		if looksUserID(handle) {
			user := api.UserResult{ID: handle, Name: handle}
			expansions = appendUniqueUserExpansion(expansions, originalSearchToken(operator, handle), user)
			return prefix + operator + "<@" + handle + ">"
		}
		key := strings.ToLower(handle)
		user, ok := resolved[key]
		if !ok {
			user, ok = resolve(handle)
			if ok {
				resolved[key] = user
				expansions = appendUniqueUserExpansion(expansions, originalSearchToken(operator, handle), user)
			}
		}
		if !ok || user.ID == "" {
			return original
		}
		return prefix + operator + "<@" + user.ID + ">"
	}
	expanded := searchUserHandlePattern.ReplaceAllStringFunc(query, func(match string) string {
		parts := searchUserHandlePattern.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		prefix, operator, handle := parts[1], parts[2], parts[3]
		return expand(prefix, operator, handle, match)
	})
	expanded = searchFromUserPattern.ReplaceAllStringFunc(expanded, func(match string) string {
		parts := searchFromUserPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		prefix, handle := parts[1], parts[2]
		if strings.HasPrefix(handle, "<") {
			return match
		}
		return expand(prefix, "from:", handle, match)
	})
	expanded = searchBareUserIDPattern.ReplaceAllStringFunc(expanded, func(match string) string {
		parts := searchBareUserIDPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		prefix, userID := parts[1], parts[2]
		return expand(prefix, "", userID, match)
	})
	if expanded == query {
		if handle, ok := bareUserSearchTerm(query); ok {
			expanded = expand("", "", handle, query)
		}
	}
	return expanded, expansions
}

func bareUserSearchTerm(query string) (string, bool) {
	query = strings.TrimSpace(query)
	if query == "" || strings.ContainsAny(query, " \t\r\n\"'") {
		return "", false
	}
	if strings.Contains(query, ":") || strings.HasPrefix(query, "#") || strings.HasPrefix(query, "@") {
		return "", false
	}
	for _, r := range query {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return "", false
	}
	return query, true
}

func appendUniqueUserExpansion(expansions []searchUserExpansion, handle string, user api.UserResult) []searchUserExpansion {
	for _, expansion := range expansions {
		if expansion.Handle == handle && expansion.UserID == user.ID {
			return expansions
		}
	}
	return append(expansions, searchUserExpansion{
		Handle: handle,
		UserID: user.ID,
		Name:   userSearchLabel(user),
	})
}

func originalSearchToken(operator string, handle string) string {
	if operator == "from:" {
		return "from:" + strings.TrimPrefix(handle, "@")
	}
	if strings.HasPrefix(handle, "@") {
		return handle
	}
	return "@" + handle
}

func expandSearchChannels(query string, resolve func(target string) (api.ChannelResult, bool)) (string, []searchChannelExpansion) {
	expansions := []searchChannelExpansion{}
	expanded := searchChannelPattern.ReplaceAllStringFunc(query, func(match string) string {
		parts := searchChannelPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		prefix, target := parts[1], parts[2]
		channel, ok := resolve(target)
		if !ok || channel.Name == "" {
			return match
		}
		expansions = appendUniqueChannelExpansion(expansions, target, channel)
		return prefix + "in:" + channel.Name
	})
	return expanded, expansions
}

func appendUniqueChannelExpansion(expansions []searchChannelExpansion, target string, channel api.ChannelResult) []searchChannelExpansion {
	for _, expansion := range expansions {
		if expansion.Target == target && expansion.ChannelID == channel.ID {
			return expansions
		}
	}
	return append(expansions, searchChannelExpansion{
		Target:    target,
		ChannelID: channel.ID,
		Name:      channel.Name,
	})
}

func resolveSearchUserHandle(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, handle string) (api.UserResult, bool) {
	user, _, ok := resolveSearchUserHandleWithSuggestions(ctx, globals, cacheDB, client, handle)
	return user, ok
}

func resolveSearchUserHandleWithSuggestions(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, handle string) (api.UserResult, []api.UserResult, bool) {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return api.UserResult{}, nil, false
	}
	if looksUserID(handle) {
		return api.UserResult{ID: handle, Name: handle}, nil, true
	}
	if cacheDB != nil {
		if user, found, err := cacheDB.UserByName(handle); err == nil && found {
			return user, nil, true
		}
	}
	users, err := client.UsersList(ctx, 1000)
	if err != nil {
		return api.UserResult{}, nil, false
	}
	for _, user := range users {
		if cacheDB != nil && !globals.NoCache {
			_ = cacheDB.UpsertUser(user)
		}
	}
	return matchUserHandleWithSuggestions(users, handle)
}

func matchUserHandle(users []api.UserResult, handle string) (api.UserResult, bool) {
	user, _, ok := matchUserHandleWithSuggestions(users, handle)
	return user, ok
}

func matchUserHandleWithSuggestions(users []api.UserResult, handle string) (api.UserResult, []api.UserResult, bool) {
	for _, user := range users {
		if strings.EqualFold(user.Name, handle) {
			return user, nil, true
		}
	}
	matches := []api.UserResult{}
	for _, user := range users {
		if strings.EqualFold(user.DisplayName, handle) || strings.EqualFold(user.RealName, handle) {
			matches = append(matches, user)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil, true
	}
	ranked := rankedUserCandidates(users, handle)
	if len(ranked) == 0 {
		return api.UserResult{}, nil, false
	}
	best := []userCandidate{}
	for _, candidate := range ranked {
		if candidate.score != ranked[0].score {
			break
		}
		best = append(best, candidate)
	}
	suggestions := userCandidates(ranked, 5)
	if len(best) != 1 {
		return api.UserResult{}, suggestions, false
	}
	return best[0].user, suggestions, true
}

type userCandidate struct {
	user  api.UserResult
	score int
}

func rankedUserCandidates(users []api.UserResult, handle string) []userCandidate {
	input := normalizeLookupValue(handle)
	bestByID := map[string]userCandidate{}
	for _, user := range users {
		for _, value := range []string{user.Name, user.DisplayName, user.RealName} {
			score, ok := userMatchScore(input, value)
			if !ok {
				continue
			}
			id := firstNonEmpty(user.ID, user.Name, user.DisplayName, user.RealName)
			if current, found := bestByID[id]; !found || score < current.score {
				bestByID[id] = userCandidate{user: user, score: score}
			}
		}
	}
	ranked := make([]userCandidate, 0, len(bestByID))
	for _, candidate := range bestByID {
		ranked = append(ranked, candidate)
	}
	sort.SliceStable(ranked, func(left int, right int) bool {
		if ranked[left].score != ranked[right].score {
			return ranked[left].score < ranked[right].score
		}
		return userSearchLabel(ranked[left].user) < userSearchLabel(ranked[right].user)
	})
	return ranked
}

func userMatchScore(input string, candidate string) (int, bool) {
	candidate = normalizeLookupValue(candidate)
	if input == "" || candidate == "" {
		return 0, false
	}
	if input == candidate {
		return 0, true
	}
	if strings.HasPrefix(candidate, input) {
		return 10 + len(candidate) - len(input), true
	}
	if strings.Contains(candidate, input) {
		return 20 + len(candidate) - len(input), true
	}
	score, ok := fuzzyMatchScore(input, candidate)
	if !ok {
		return 0, false
	}
	return 40 + score, true
}

func userCandidates(candidates []userCandidate, limit int) []api.UserResult {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	users := make([]api.UserResult, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		users = append(users, candidate.user)
		if len(users) >= limit {
			return users
		}
	}
	return users
}

func userSuggestionLabels(users []api.UserResult) []string {
	labels := make([]string, 0, len(users))
	for _, user := range users {
		parts := []string{}
		if user.Name != "" {
			parts = append(parts, "@"+strings.TrimPrefix(user.Name, "@"))
		}
		if display := firstNonEmpty(user.DisplayName, user.RealName); display != "" {
			parts = append(parts, display)
		}
		if user.ID != "" {
			parts = append(parts, user.ID)
		}
		if len(parts) > 0 {
			labels = append(labels, strings.Join(parts, " - "))
		}
	}
	return labels
}

func userSuggestionCommands(users []api.UserResult) []string {
	commands := make([]string, 0, len(users))
	seen := map[string]bool{}
	for _, user := range users {
		target := ""
		if user.Name != "" {
			target = "@" + strings.TrimPrefix(user.Name, "@")
		} else if user.ID != "" {
			target = user.ID
		}
		if target == "" {
			continue
		}
		command := "slacky user " + shellQuote(target) + " --json"
		if seen[command] {
			continue
		}
		seen[command] = true
		commands = append(commands, command)
	}
	return commands
}

func userSearchLabel(user api.UserResult) string {
	return firstNonEmpty(user.DisplayName, user.RealName, user.Name)
}

func resolveSearchChannel(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, target string) (api.ChannelResult, bool) {
	if channelID, ok := channelIDFromTarget(target); ok {
		if cacheDB != nil {
			if channels, err := cacheDB.Channels(channelID, 1); err == nil && len(channels) > 0 {
				return channels[0], true
			}
		}
		if channel, err := client.ConversationInfo(ctx, channelID); err == nil {
			if cacheDB != nil && !globals.NoCache {
				_ = cacheDB.UpsertChannels([]api.ChannelResult{channel})
			}
			return channel, true
		}
		return api.ChannelResult{}, false
	}
	if cacheDB != nil {
		if channels, err := cacheDB.Channels(target, 1); err == nil && len(channels) > 0 {
			return channels[0], true
		}
	}
	channels, err := client.ConversationsList(ctx, 0)
	if err != nil {
		return api.ChannelResult{}, false
	}
	if cacheDB != nil && !globals.NoCache {
		_ = cacheDB.UpsertChannels(channels)
	}
	matches := filterChannels(channels, target)
	if len(matches) != 1 {
		return api.ChannelResult{}, false
	}
	return matches[0], true
}

func messagesForHuman(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, messages []api.MessageResult, expansions []searchUserExpansion) []api.MessageResult {
	userLabels := displayUserLabels(ctx, globals, cacheDB, client, messages, expansions)
	channelNames := displayChannelNames(ctx, globals, cacheDB, client, messages)
	return messagesWithDisplayChannels(messagesWithDisplayMentions(messages, userLabels), channelNames)
}

func threadForHuman(ctx context.Context, globals *Globals, client *api.Client, thread api.ThreadResult) api.ThreadResult {
	thread.Messages = messagesForHuman(ctx, globals, nil, client, thread.Messages, nil)
	if thread.ChannelName == "" {
		for index := range thread.Messages {
			message := thread.Messages[index]
			if message.ChannelName != "" {
				thread.ChannelName = message.ChannelName
				break
			}
		}
	}
	return thread
}

func displayUserLabels(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, messages []api.MessageResult, expansions []searchUserExpansion) map[string]string {
	labels := displayUserLabelsFromExpansions(expansions)
	ids := collectMessageUserIDs(messages)
	if len(ids) == 0 {
		return labels
	}
	ownedCache := false
	if cacheDB == nil && (globals == nil || !globals.NoCache) {
		if pathSet, err := paths.Resolve(); err == nil {
			if client != nil && ctx != nil {
				cacheDB, _ = store.Open(pathSet.CacheDB.Path)
			} else {
				cacheDB, _ = store.OpenReadOnly(pathSet.CacheDB.Path)
			}
			ownedCache = cacheDB != nil
		}
	}
	if ownedCache {
		defer func() {
			_ = cacheDB.Close()
		}()
	}
	for _, id := range ids {
		if labels[id] != "" {
			continue
		}
		if cacheDB != nil {
			if user, found, err := cacheDB.UserByID(id); err == nil && found {
				labels[id] = "@" + userMentionLabel(user)
				continue
			}
		}
		if client == nil || ctx == nil {
			continue
		}
		user, err := client.UserInfo(ctx, id)
		if err != nil || user.ID == "" {
			continue
		}
		labels[id] = "@" + userMentionLabel(user)
		if cacheDB != nil && (globals == nil || !globals.NoCache) {
			_ = cacheDB.UpsertUser(user)
		}
	}
	return labels
}

func displayChannelNames(ctx context.Context, globals *Globals, cacheDB *store.DB, client *api.Client, messages []api.MessageResult) map[string]string {
	ids := collectMessageChannelIDs(messages)
	if len(ids) == 0 {
		return map[string]string{}
	}
	names := map[string]string{}
	ownedCache := false
	if cacheDB == nil && (globals == nil || !globals.NoCache) {
		if pathSet, err := paths.Resolve(); err == nil {
			if client != nil && ctx != nil {
				cacheDB, _ = store.Open(pathSet.CacheDB.Path)
			} else {
				cacheDB, _ = store.OpenReadOnly(pathSet.CacheDB.Path)
			}
			ownedCache = cacheDB != nil
		}
	}
	if ownedCache {
		defer func() {
			_ = cacheDB.Close()
		}()
	}
	for _, id := range ids {
		if cacheDB != nil {
			if channels, err := cacheDB.Channels(id, 1); err == nil && len(channels) > 0 && channels[0].Name != "" {
				names[id] = channels[0].Name
				continue
			}
		}
		if client == nil || ctx == nil {
			continue
		}
		channel, err := client.ConversationInfo(ctx, id)
		if err != nil || channel.Name == "" {
			continue
		}
		names[id] = channel.Name
		if cacheDB != nil && (globals == nil || !globals.NoCache) {
			_ = cacheDB.UpsertChannels([]api.ChannelResult{channel})
		}
	}
	return names
}

func displayUserLabelsFromExpansions(expansions []searchUserExpansion) map[string]string {
	labels := map[string]string{}
	for _, expansion := range expansions {
		if expansion.UserID == "" {
			continue
		}
		label := firstNonEmpty(expansion.Name, strings.TrimPrefix(expansion.Handle, "from:"), expansion.UserID)
		label = strings.TrimPrefix(strings.TrimSpace(label), "@")
		if label == "" {
			continue
		}
		labels[expansion.UserID] = "@" + label
	}
	return labels
}

func collectMessageUserIDs(messages []api.MessageResult) []string {
	seen := map[string]bool{}
	ids := []string{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if !looksUserID(id) || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for index := range messages {
		message := messages[index]
		add(message.User)
		for _, value := range []string{message.Excerpt, message.TextFull, api.RichTextFromParts("", message.Blocks, message.Attachments, message.Files)} {
			for _, match := range displayUserMentionPattern.FindAllStringSubmatch(value, -1) {
				if len(match) >= 2 && match[1] != "" {
					add(match[1])
				} else if len(match) >= 3 {
					add(match[2])
				}
			}
		}
	}
	return ids
}

func collectMessageChannelIDs(messages []api.MessageResult) []string {
	seen := map[string]bool{}
	ids := []string{}
	for index := range messages {
		message := messages[index]
		if message.ChannelName != "" || !looksChannelID(message.ChannelID) || seen[message.ChannelID] {
			continue
		}
		seen[message.ChannelID] = true
		ids = append(ids, message.ChannelID)
	}
	return ids
}

func messagesWithDisplayMentions(messages []api.MessageResult, labels map[string]string) []api.MessageResult {
	replacer := displayMentionReplacer(labels)
	if replacer == nil {
		return messages
	}
	displayMessages := make([]api.MessageResult, len(messages))
	for index := range messages {
		displayMessages[index] = messages[index]
		displayMessages[index].Excerpt = replacer.Replace(displayMessages[index].Excerpt)
		displayMessages[index].TextFull = replacer.Replace(displayMessages[index].TextFull)
		if label := labels[displayMessages[index].User]; label != "" {
			username := strings.TrimPrefix(displayMessages[index].Username, "@")
			if username == "" || looksUserID(username) {
				displayMessages[index].Username = strings.TrimPrefix(label, "@")
			}
		}
	}
	return displayMessages
}

func messagesWithDisplayChannels(messages []api.MessageResult, names map[string]string) []api.MessageResult {
	if len(names) == 0 {
		return messages
	}
	displayMessages := make([]api.MessageResult, len(messages))
	for index := range messages {
		displayMessages[index] = messages[index]
		if displayMessages[index].ChannelName == "" {
			displayMessages[index].ChannelName = names[displayMessages[index].ChannelID]
		}
	}
	return displayMessages
}

func displayMentionReplacer(labels map[string]string) *strings.Replacer {
	if len(labels) == 0 {
		return nil
	}
	ids := make([]string, 0, len(labels))
	for id := range labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	values := []string{}
	for _, id := range ids {
		label := labels[id]
		if id == "" || label == "" {
			continue
		}
		values = append(values, "<@"+id+">", label, "@"+id, label)
	}
	if len(values) == 0 {
		return nil
	}
	return strings.NewReplacer(values...)
}

func userMentionLabel(user api.UserResult) string {
	return strings.TrimPrefix(firstNonEmpty(userSearchLabel(user), user.ID), "@")
}

func fuzzyMatchScore(input string, candidate string) (int, bool) {
	input = normalizeLookupValue(input)
	candidate = normalizeLookupValue(candidate)
	if input == "" || candidate == "" {
		return 0, false
	}
	if input == candidate {
		return 0, true
	}
	score := damerauLevenshtein(input, candidate)
	maxDistance := 1
	if len(input) >= 4 {
		maxDistance = 2
	}
	if len(input) >= 8 {
		maxDistance = 3
	}
	return score, score <= maxDistance
}

func normalizeLookupValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "@")
	value = strings.TrimPrefix(value, "#")
	return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(value)
}

func damerauLevenshtein(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	distances := make([][]int, len(leftRunes)+1)
	for index := range distances {
		distances[index] = make([]int, len(rightRunes)+1)
		distances[index][0] = index
	}
	for index := range distances[0] {
		distances[0][index] = index
	}
	for i := 1; i <= len(leftRunes); i++ {
		for j := 1; j <= len(rightRunes); j++ {
			cost := 1
			if leftRunes[i-1] == rightRunes[j-1] {
				cost = 0
			}
			distances[i][j] = min(
				distances[i-1][j]+1,
				distances[i][j-1]+1,
				distances[i-1][j-1]+cost,
			)
			if i > 1 && j > 1 && leftRunes[i-1] == rightRunes[j-2] && leftRunes[i-2] == rightRunes[j-1] {
				distances[i][j] = min(distances[i][j], distances[i-2][j-2]+1)
			}
		}
	}
	return distances[len(leftRunes)][len(rightRunes)]
}

func looksUserID(value string) bool {
	if len(value) < 2 {
		return false
	}
	first := value[0]
	if first != 'U' && first != 'W' {
		return false
	}
	for _, r := range value[1:] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func looksChannelID(value string) bool {
	if len(value) < 2 {
		return false
	}
	first := value[0]
	if first != 'C' && first != 'G' && first != 'D' {
		return false
	}
	for _, r := range value[1:] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func liveSearchHeader(query string, slackQuery string, showing string) []string {
	header := []string{fmt.Sprintf("Query: %s", query)}
	if slackQuery != query {
		header = append(header, fmt.Sprintf("Expanded: %s", slackQuery))
	}
	return append(header, showing)
}

func renderSearchMessageOutput(evidence bool, header []string, messages []api.MessageResult, query string, highlightTerms []string, options messageRenderOptions) string {
	if evidence {
		return renderMessageListWithOptions("Search", header, messages, options)
	}
	return renderSearchMessageList(header, messages, query, highlightTerms)
}

func renderSearchMessageList(header []string, messages []api.MessageResult, query string, highlightTerms []string) string {
	lines := []string{"Search", ""}
	lines = append(lines, header...)
	if len(header) > 0 {
		lines = append(lines, "")
	}
	if len(messages) == 0 {
		lines = append(lines, "No messages found.", "")
		lines = append(lines, "Next:", "  slacky search "+shellQuote(query)+" --json")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, searchMessageTableLines(messages, highlightTerms)...)
	lines = appendSearchActionBlocks(lines, messages, query)
	return strings.Join(lines, "\n")
}

func searchMessageTableLines(messages []api.MessageResult, highlightTerms []string) []string {
	const (
		refWidth     = 3
		channelWidth = 18
		userWidth    = 16
		timeWidth    = 16
	)
	matchWidth := searchMatchWidth(refWidth, channelWidth, userWidth, timeWidth)
	lines := make([]string, 0, 2+len(messages))
	lines = append(
		lines,
		searchTableRow("REF", "CHANNEL", "USER", "TIME", "MATCH", refWidth, channelWidth, userWidth, timeWidth),
		searchTableRow("---", "-------", "----", "----", "-----", refWidth, channelWidth, userWidth, timeWidth),
	)
	for index := range messages {
		message := messages[index]
		match := output.Truncate(messageText(message), matchWidth)
		match = output.HighlightTerms(match, highlightTerms)
		lines = append(lines, searchTableRow(
			strconv.Itoa(index+1),
			messageChannelLabel(message),
			firstNonEmpty(messageSender(message), "-"),
			slackTSLabel(message.TS),
			match,
			refWidth,
			channelWidth,
			userWidth,
			timeWidth,
		))
	}
	return lines
}

func searchTableRow(ref string, channel string, user string, ts string, match string, refWidth int, channelWidth int, userWidth int, timeWidth int) string {
	return fmt.Sprintf(
		"  %-*s  %-*s  %-*s  %-*s  %s",
		refWidth,
		output.Truncate(ref, refWidth),
		channelWidth,
		output.Truncate(channel, channelWidth),
		userWidth,
		output.Truncate(user, userWidth),
		timeWidth,
		output.Truncate(ts, timeWidth),
		match,
	)
}

func searchMatchWidth(refWidth int, channelWidth int, userWidth int, timeWidth int) int {
	width := output.TerminalWidth() - 2 - refWidth - channelWidth - userWidth - timeWidth - 8
	if width < 30 {
		return 30
	}
	return width
}

func appendSearchActionBlocks(lines []string, messages []api.MessageResult, query string) []string {
	lines = append(lines, "", "Open:")
	for _, command := range topSearchMessageCommands(messages, threadCommand, 3) {
		lines = append(lines, "  "+command)
	}
	lines = append(lines, "Context:")
	for _, command := range topSearchMessageCommands(messages, contextCommand, 3) {
		lines = append(lines, "  "+command)
	}
	quoted := shellQuote(query)
	lines = append(lines, "More:", "  slacky search "+quoted+" --evidence", "  slacky search "+quoted+" --json")
	return lines
}

func topSearchMessageCommands(messages []api.MessageResult, commandFor func(api.MessageResult) string, limit int) []string {
	commands := []string{}
	seen := map[string]bool{}
	for index := range messages {
		command := commandFor(messages[index])
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		commands = append(commands, fmt.Sprintf("%s  # %d", command, index+1))
		if len(commands) >= limit {
			return commands
		}
	}
	return commands
}

func searchHighlightTerms(query string, userExpansions []searchUserExpansion, channelExpansions []searchChannelExpansion) []string {
	terms := make([]string, 0, len(strings.Fields(query))+len(userExpansions)*3+len(channelExpansions)*3)
	for _, term := range strings.Fields(query) {
		term = strings.Trim(term, "\"'`.,:;()[]{}")
		if after, ok := strings.CutPrefix(term, "from:"); ok {
			term = after
		}
		if after, ok := strings.CutPrefix(term, "in:"); ok {
			term = after
		}
		terms = append(terms, strings.TrimPrefix(strings.TrimPrefix(term, "@"), "#"))
	}
	for _, expansion := range userExpansions {
		handle := strings.TrimPrefix(strings.TrimPrefix(expansion.Handle, "from:"), "@")
		terms = append(terms, handle, expansion.Name, "@"+expansion.Name)
	}
	for _, expansion := range channelExpansions {
		target := strings.TrimPrefix(expansion.Target, "#")
		terms = append(terms, target, expansion.Name, "#"+expansion.Name)
	}
	return terms
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == ',' || r == '+' || r == '=' || r == '@' || r == '#' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			continue
		}
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return value
}

func renderMessageListWithOptions(title string, header []string, messages []api.MessageResult, options messageRenderOptions) string {
	lines := []string{title, ""}
	lines = append(lines, header...)
	if len(header) > 0 {
		lines = append(lines, "")
	}
	if len(messages) == 0 {
		lines = append(lines, "No messages found.")
	} else {
		width := readableContentWidth()
		for index := range messages {
			message := messages[index]
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, messageTitle(message)))
			if meta := messageMeta(message); meta != "" {
				lines = append(lines, "   "+meta)
			}
			lines = append(lines, "   Text:")
			for _, line := range wrapRenderedText(messageTextWithOptions(message, options), width) {
				lines = append(lines, "     "+line)
			}
			lines = appendCommandBlock(lines, "   ", "Thread", threadCommand(message))
			lines = appendCommandBlock(lines, "   ", "Context", contextCommand(message))
			lines = append(lines, "")
		}
	}
	lines = append(lines, "Next:", "  slacky search --json <query>", "  slacky thread --channel <channel-id> --ts <root-ts>", "  slacky context --channel <channel-id> --ts <ts>")
	return strings.Join(lines, "\n")
}

func renderThreadListWithOptions(title string, header []string, threads []api.ThreadResult, options messageRenderOptions) string {
	lines := []string{title, ""}
	lines = append(lines, header...)
	if len(header) > 0 {
		lines = append(lines, "")
	}
	if len(threads) == 0 {
		lines = append(lines, "No threads found.")
	} else {
		width := readableContentWidth()
		for index, thread := range threads {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, threadTitle(thread)))
			lines = append(lines, fmt.Sprintf("   Messages: %d", len(thread.Messages)))
			if len(thread.Messages) > 0 {
				lines = append(lines, "   First:")
				for _, line := range wrapRenderedText(messageTextWithOptions(thread.Messages[0], options), width) {
					lines = append(lines, "     "+line)
				}
			}
			lines = appendCommandBlock(lines, "   ", "Open", threadOpenCommand(thread))
			lines = append(lines, "")
		}
	}
	lines = append(lines, "Next:", "  slacky thread --channel <channel-id> --ts <root-ts>", "  slacky search --json <query>")
	return strings.Join(lines, "\n")
}

func wrapRenderedText(text string, width int) []string {
	if strings.TrimSpace(text) == "" {
		return []string{"(empty)"}
	}
	lines := []string{}
	for _, part := range strings.Split(text, "\n") {
		if strings.TrimSpace(part) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, output.Wrap(part, width)...)
	}
	return lines
}

func appendCommandBlock(lines []string, indent string, label string, commands ...string) []string {
	lines = append(lines, indent+label+":")
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		lines = append(lines, indent+"  "+command)
	}
	return lines
}

func renderChannelList(target string, channels []api.ChannelResult) string {
	lines := []string{"Channels", ""}
	if target != "" {
		lines = append(lines, fmt.Sprintf("Target: %s", target), "")
	}
	if len(channels) == 0 {
		lines = append(lines, "No channels found.")
	} else {
		lines = append(lines, "  CHANNEL                       ID             VISIBILITY  MEMBERS")
		lines = append(lines, "  -------                       --             ----------  -------")
		for _, channel := range channels {
			lines = append(lines, fmt.Sprintf(
				"  %-29s %-14s %-10s %7s",
				output.Truncate(channelLabel(channel), 29),
				output.Truncate(channel.ID, 14),
				output.Truncate(channelVisibility(channel), 10),
				channelMembers(channel),
			))
		}
	}
	lines = append(lines, "", "Next:", "  slacky history --channel <channel-id>", "  slacky search \"in:<channel-name> topic\"")
	return strings.Join(lines, "\n")
}

func readableContentWidth() int {
	width := output.TerminalWidth() - 6
	if width > 96 {
		width = 96
	}
	if width < 48 {
		width = 48
	}
	return width
}

func messageTitle(message api.MessageResult) string {
	parts := []string{messageChannelLabel(message)}
	if sender := messageSender(message); sender != "" {
		parts = append(parts, sender)
	}
	if timestamp := slackTSLabel(message.TS); timestamp != "" {
		parts = append(parts, timestamp)
	}
	return strings.Join(parts, "  ")
}

func threadTitle(thread api.ThreadResult) string {
	parts := []string{messageChannelLabel(api.MessageResult{ChannelID: thread.ChannelID, ChannelName: thread.ChannelName})}
	if timestamp := slackTSLabel(thread.RootTS); timestamp != "" {
		parts = append(parts, timestamp)
	}
	return strings.Join(parts, "  ")
}

func messageMeta(message api.MessageResult) string {
	parts := []string{}
	if message.ChannelID != "" {
		parts = append(parts, "channel="+message.ChannelID)
	}
	if message.TS != "" {
		parts = append(parts, "ts="+message.TS)
	}
	if root := messageRootTS(message); root != "" && root != message.TS {
		parts = append(parts, "root="+root)
	}
	return strings.Join(parts, "  ")
}

func messageText(message api.MessageResult) string {
	return messageTextWithOptions(message, messageRenderOptions{})
}

func messageTextWithOptions(message api.MessageResult, options messageRenderOptions) string {
	if options.Verbose {
		return fullMessageText(message, options)
	}
	text := excerptMessageText(message)
	if text == "" {
		return "(empty)"
	}
	return text
}

func excerptMessageText(message api.MessageResult) string {
	text := firstNonEmpty(message.Excerpt, message.TextFull, api.RichTextFromParts("", message.Blocks, message.Attachments, message.Files))
	text = slackCodeFencePattern.ReplaceAllString(text, " ")
	return output.Truncate(api.CleanSlackText(text), 300)
}

func fullMessageText(message api.MessageResult, options messageRenderOptions) string {
	main := firstNonEmpty(message.TextFull, message.Excerpt, api.RichTextFromParts("", message.Blocks, message.Attachments, message.Files))
	if options.IncludeRichContent && message.Excerpt != "" && (message.TextFull == "" || strings.Contains(message.Excerpt, "\n")) {
		main = message.Excerpt
	}
	main = output.Truncate(api.CleanSlackText(main), 2000)
	parts := []string{}
	if main != "" {
		parts = append(parts, main)
	}
	if options.IncludeRichContent {
		parts = appendRichTextSection(parts, "blocks", api.RichBlocksText(message.Blocks), main)
		parts = appendRichTextSection(parts, "attachments", api.RichAttachmentsText(message.Attachments), main)
		parts = appendRichTextSection(parts, "files", api.RichFilesText(message.Files), main)
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, "\n")
}

func appendRichTextSection(parts []string, label string, content string, main string) []string {
	content = cleanMultilineSlackText(content)
	if content == "" || richContentAlreadyShown(main, content) {
		return parts
	}
	parts = append(parts, "--- "+label+" ---", content)
	return parts
}

func cleanMultilineSlackText(content string) string {
	lines := []string{}
	for _, line := range strings.Split(content, "\n") {
		line = api.CleanSlackText(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func richContentAlreadyShown(main string, content string) bool {
	main = strings.TrimSpace(api.CleanSlackText(main))
	content = strings.TrimSpace(api.CleanSlackText(content))
	if main == "" || content == "" {
		return false
	}
	return main == content || strings.Contains(main, content)
}

func messageChannelLabel(message api.MessageResult) string {
	if message.ChannelName != "" {
		return "#" + message.ChannelName
	}
	return blank(message.ChannelID)
}

func messageSender(message api.MessageResult) string {
	if message.Username != "" {
		return "@" + strings.TrimPrefix(message.Username, "@")
	}
	if message.User != "" {
		return "@" + message.User
	}
	return ""
}

func messageRootTS(message api.MessageResult) string {
	return firstNonEmpty(message.RootTS, message.ThreadTS, message.TS)
}

func compactMessageResults(messages []api.MessageResult) []compactMessageResult {
	results := make([]compactMessageResult, 0, len(messages))
	for index := range messages {
		results = append(results, compactMessageResultFor(index+1, messages[index]))
	}
	return results
}

func compactMessageResultFor(ref int, message api.MessageResult) compactMessageResult {
	message = enrichMessageForJSON(message)
	return compactMessageResult{
		Ref:         ref,
		ChannelID:   message.ChannelID,
		ChannelName: message.ChannelName,
		TS:          message.TS,
		ThreadTS:    message.ThreadTS,
		RootTS:      messageRootTS(message),
		Permalink:   message.Permalink,
		User:        message.User,
		Username:    message.Username,
		DisplayName: message.DisplayName,
		Datetime:    message.Datetime,
		Date:        message.Date,
		Excerpt:     excerptMessageText(message),
		Commands:    messageCommands(message),
	}
}

func compactThreadResults(threads []api.ThreadResult) []compactThreadResult {
	results := make([]compactThreadResult, 0, len(threads))
	for index := range threads {
		results = append(results, compactThreadResultFor(index+1, threads[index], false))
	}
	return results
}

func compactThreadResultFor(ref int, thread api.ThreadResult, includeMessages bool) compactThreadResult {
	thread = enrichThreadForJSON(thread)
	result := compactThreadResult{
		Ref:          ref,
		ChannelID:    thread.ChannelID,
		ChannelName:  thread.ChannelName,
		RootTS:       thread.RootTS,
		Datetime:     thread.Datetime,
		Date:         thread.Date,
		Permalink:    thread.Permalink,
		MessageCount: len(thread.Messages),
		Commands: resultCommands{
			Thread: threadOpenCommand(thread),
			Open:   openCommand(thread.Permalink),
		},
	}
	if len(thread.Messages) == 0 {
		return result
	}
	if includeMessages {
		result.Messages = compactMessageResults(thread.Messages)
		return result
	}
	result.First = compactMessageResultFor(1, thread.Messages[0])
	return result
}

func messageCommands(message api.MessageResult) resultCommands {
	commands := resultCommands{
		Message: messageCommand(message),
		Thread:  threadCommand(message),
		Context: contextCommand(message),
		Open:    openCommand(message.Permalink),
	}
	if root := messageRootTS(message); root != "" && root != message.TS {
		commands.RootContext = fmt.Sprintf("slacky context --channel %s --ts %s", blank(message.ChannelID), blank(root))
	}
	return commands
}

func messageCommand(message api.MessageResult) string {
	return fmt.Sprintf("slacky message --channel %s --ts %s", blank(message.ChannelID), blank(message.TS))
}

func threadCommand(message api.MessageResult) string {
	return fmt.Sprintf("slacky thread --channel %s --ts %s", blank(message.ChannelID), blank(messageRootTS(message)))
}

func contextCommand(message api.MessageResult) string {
	return fmt.Sprintf("slacky context --channel %s --ts %s", blank(message.ChannelID), blank(message.TS))
}

func threadOpenCommand(thread api.ThreadResult) string {
	return fmt.Sprintf("slacky thread --channel %s --ts %s", blank(thread.ChannelID), blank(thread.RootTS))
}

func openCommand(permalink string) string {
	if strings.TrimSpace(permalink) == "" {
		return ""
	}
	return "slacky open " + shellQuote(permalink)
}

func slackTSLabel(ts string) string {
	if ts == "" {
		return ""
	}
	secondsText := strings.SplitN(ts, ".", 2)[0]
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil {
		return ts
	}
	return time.Unix(seconds, 0).Local().Format("2006-01-02 15:04")
}

func channelLabel(channel api.ChannelResult) string {
	if channel.Name != "" {
		return "#" + channel.Name
	}
	return channel.ID
}

func channelVisibility(channel api.ChannelResult) string {
	switch {
	case channel.IsIM:
		return "dm"
	case channel.IsMPIM:
		return "group-dm"
	case channel.IsPrivate:
		return "private"
	default:
		return "public"
	}
}

func channelMembers(channel api.ChannelResult) string {
	if channel.NumMembers <= 0 {
		return "-"
	}
	return strconv.Itoa(channel.NumMembers)
}

func filterChannels(channels []api.ChannelResult, target string) []api.ChannelResult {
	if target == "" {
		return channels
	}
	if channelID, ok := channelIDFromTarget(target); ok {
		filtered := make([]api.ChannelResult, 0, 1)
		for _, channel := range channels {
			if strings.EqualFold(channel.ID, channelID) {
				filtered = append(filtered, channel)
			}
		}
		return filtered
	}
	cleanTarget := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(target)), "#")
	filtered := make([]api.ChannelResult, 0, len(channels))
	for _, channel := range channels {
		if strings.EqualFold(channel.ID, target) || strings.EqualFold(channel.Name, cleanTarget) {
			filtered = append(filtered, channel)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}
	return fuzzyChannels(channels, target)
}

func fuzzyChannels(channels []api.ChannelResult, target string) []api.ChannelResult {
	type candidate struct {
		channel api.ChannelResult
		score   int
	}
	best := []candidate{}
	for _, channel := range channels {
		score, ok := fuzzyMatchScore(target, channel.Name)
		if !ok {
			continue
		}
		if len(best) == 0 || score < best[0].score {
			best = []candidate{{channel: channel, score: score}}
			continue
		}
		if score == best[0].score && channel.ID != best[0].channel.ID {
			best = append(best, candidate{channel: channel, score: score})
		}
	}
	if len(best) != 1 {
		return nil
	}
	return []api.ChannelResult{best[0].channel}
}

func groupMessagesByThread(messages []api.MessageResult) []api.ThreadResult {
	order := []string{}
	grouped := map[string][]api.MessageResult{}
	for index := range messages {
		message := messages[index]
		root := message.RootTS
		if root == "" {
			root = message.TS
		}
		key := message.ChannelID + "/" + root
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], message)
	}
	threads := make([]api.ThreadResult, 0, len(order))
	for _, key := range order {
		messages := grouped[key]
		if len(messages) == 0 {
			continue
		}
		threads = append(threads, api.ThreadResult{
			ChannelID:   messages[0].ChannelID,
			ChannelName: messages[0].ChannelName,
			RootTS:      messages[0].RootTS,
			Permalink:   messages[0].Permalink,
			Messages:    messages,
		})
	}
	return threads
}

func mergeMessages(primary []api.MessageResult, secondary []api.MessageResult) []api.MessageResult {
	merged := make([]api.MessageResult, 0, len(primary)+len(secondary))
	seen := map[string]bool{}
	add := func(message api.MessageResult) {
		key := message.ChannelID + "/" + message.TS
		if key == "/" || seen[key] {
			return
		}
		seen[key] = true
		merged = append(merged, message)
	}
	for index := range primary {
		add(primary[index])
	}
	for index := range secondary {
		add(secondary[index])
	}
	return merged
}

func reverseMessages(messages []api.MessageResult) []api.MessageResult {
	reversed := make([]api.MessageResult, len(messages))
	for index := range messages {
		reversed[len(messages)-1-index] = messages[index]
	}
	return reversed
}

func channelsFromMessages(messages []api.MessageResult) []api.ChannelResult {
	seen := map[string]bool{}
	var channels []api.ChannelResult
	for index := range messages {
		message := messages[index]
		if message.ChannelID == "" || seen[message.ChannelID] {
			continue
		}
		seen[message.ChannelID] = true
		channels = append(channels, api.ChannelResult{
			ID:        message.ChannelID,
			Name:      message.ChannelName,
			IsChannel: true,
		})
	}
	return channels
}
