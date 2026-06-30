package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/store"
)

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
	Local              bool     `help:"Rank only cached local messages without calling Slack" name:"local"`
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
	local := localFindSearch(pathSet.CacheDB.Path, topic, cmd.Count, cmd.Local || !globals.NoCache)
	if cmd.Local {
		return writeLocalFindEnvelope(globals, topic, cmd, renderOptions, local, store.Inspect(pathSet.CacheDB.Path))
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
					"local":       false,
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
			"local":                false,
			"verbose":              cmd.Verbose,
			"include_rich_content": cmd.IncludeRichContent,
			"suggestions":          local.Suggestions,
		},
		Cache: cacheStatus,
	})
}

func localFindSearch(cachePath string, topic string, count int, enabled bool) store.LocalSearchResult {
	local := store.LocalSearchResult{Messages: []api.MessageResult{}, Suggestions: []string{}}
	if !enabled {
		return local
	}
	cacheStatus := store.Inspect(cachePath)
	if !cacheStatus.Exists {
		return local
	}
	cacheDB, err := store.OpenReadOnly(cachePath)
	if err != nil {
		return local
	}
	defer func() {
		_ = cacheDB.Close()
	}()
	result, err := cacheDB.SearchMessages(topic, count)
	if err != nil {
		return local
	}
	return result
}

func writeLocalFindEnvelope(globals *Globals, topic string, cmd *FindCmd, renderOptions messageRenderOptions, local store.LocalSearchResult, cacheStatus store.Status) error {
	messages := local.Messages
	if messages == nil {
		messages = []api.MessageResult{}
	}
	displayMessages := messagesForHuman(context.TODO(), globals, nil, nil, messages, nil)
	threads := groupMessagesByThread(displayMessages)
	notice := ""
	if len(messages) == 0 {
		notice = "no cached messages matched"
	} else if len(local.Suggestions) > 0 {
		notice = "returned cached fuzzy suggestions"
	}
	header := []string{
		fmt.Sprintf("Topic: %s", topic),
		"Source: cache",
		fmt.Sprintf("Ranked cached threads: %d", len(threads)),
	}
	if notice != "" {
		header = append(header, fmt.Sprintf("Notice: %s", notice))
	}
	if len(local.Suggestions) > 0 {
		header = append(header, fmt.Sprintf("Suggestions: %s", strings.Join(local.Suggestions, ", ")))
	}
	text := renderThreadListWithOptions("Find", header, threads, renderOptions)
	return writeCompactEnvelope(globals, cmd.Compact, Envelope{
		OK:          true,
		Text:        text,
		Source:      "cache",
		CacheNotice: notice,
		Threads:     groupMessagesByThread(messages),
		Search: map[string]any{
			"topic":                topic,
			"count":                cmd.Count,
			"local":                true,
			"verbose":              cmd.Verbose,
			"include_rich_content": cmd.IncludeRichContent,
			"suggestions":          local.Suggestions,
		},
		Cache: cacheStatus,
	})
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

func liveSearchHeader(query string, slackQuery string, showing string) []string {
	header := []string{fmt.Sprintf("Query: %s", query)}
	if slackQuery != query {
		header = append(header, fmt.Sprintf("Expanded: %s", slackQuery))
	}
	return append(header, showing)
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
