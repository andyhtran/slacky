package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/resolve"
	"github.com/andyhtran/slacky/internal/store"
)

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

func reverseMessages(messages []api.MessageResult) []api.MessageResult {
	reversed := make([]api.MessageResult, len(messages))
	for index := range messages {
		reversed[len(messages)-1-index] = messages[index]
	}
	return reversed
}
