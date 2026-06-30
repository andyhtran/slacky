package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/store"
)

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
	misses := make([]string, 0, len(ids))
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
		misses = append(misses, id)
	}
	if client == nil || ctx == nil || len(misses) == 0 {
		return labels
	}
	users := lookupUsersBounded(ctx, client, misses)
	for _, id := range misses {
		user := users[id]
		if user.ID == "" {
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
	misses := make([]string, 0, len(ids))
	for _, id := range ids {
		if cacheDB != nil {
			if channels, err := cacheDB.Channels(id, 1); err == nil && len(channels) > 0 && channels[0].Name != "" {
				names[id] = channels[0].Name
				continue
			}
		}
		misses = append(misses, id)
	}
	if client == nil || ctx == nil || len(misses) == 0 {
		return names
	}
	channels := lookupChannelsBounded(ctx, client, misses)
	for _, id := range misses {
		channel := channels[id]
		if channel.Name == "" {
			continue
		}
		names[id] = channel.Name
		if cacheDB != nil && (globals == nil || !globals.NoCache) {
			_ = cacheDB.UpsertChannels([]api.ChannelResult{channel})
		}
	}
	return names
}

func lookupUsersBounded(ctx context.Context, client *api.Client, ids []string) map[string]api.UserResult {
	results := make(map[string]api.UserResult, len(ids))
	var mu sync.Mutex
	runBounded(ids, func(id string) {
		user, err := client.UserInfo(ctx, id)
		if err != nil || user.ID == "" {
			return
		}
		mu.Lock()
		results[id] = user
		mu.Unlock()
	})
	return results
}

func lookupChannelsBounded(ctx context.Context, client *api.Client, ids []string) map[string]api.ChannelResult {
	results := make(map[string]api.ChannelResult, len(ids))
	var mu sync.Mutex
	runBounded(ids, func(id string) {
		channel, err := client.ConversationInfo(ctx, id)
		if err != nil || channel.Name == "" {
			return
		}
		mu.Lock()
		results[id] = channel
		mu.Unlock()
	})
	return results
}

func runBounded(ids []string, fn func(string)) {
	const workers = 4
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, id := range ids {
		sem <- struct{}{}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(id)
		}(id)
	}
	wg.Wait()
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
