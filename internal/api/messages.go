package api

import (
	"context"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	userMentionTokenPattern    = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]+))?>`)
	channelMentionTokenPattern = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]+))?>`)
	labeledLinkTokenPattern    = regexp.MustCompile(`<((?:https?|mailto):[^>|]+)\|([^>]+)>`)
	bareLinkTokenPattern       = regexp.MustCompile(`<((?:https?|mailto):[^>]+)>`)
	specialSlackTokenPattern   = regexp.MustCompile(`<!([^>|]+)(?:\|([^>]+))?>`)
)

type ResponseMetadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
}

type MessageResult struct {
	ChannelID   string          `json:"channel_id,omitempty"`
	ChannelName string          `json:"channel_name,omitempty"`
	TS          string          `json:"ts,omitempty"`
	ThreadTS    string          `json:"thread_ts,omitempty"`
	RootTS      string          `json:"root_ts,omitempty"`
	Permalink   string          `json:"permalink,omitempty"`
	User        string          `json:"user,omitempty"`
	Username    string          `json:"username,omitempty"`
	Excerpt     string          `json:"excerpt,omitempty"`
	TextFull    string          `json:"text_full,omitempty"`
	Blocks      json.RawMessage `json:"blocks,omitempty"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
	Files       []FileResult    `json:"files,omitempty"`
}

type FileResult struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name,omitempty"`
	Title      string `json:"title,omitempty"`
	Mimetype   string `json:"mimetype,omitempty"`
	Filetype   string `json:"filetype,omitempty"`
	URLPrivate string `json:"url_private,omitempty"`
	Permalink  string `json:"permalink,omitempty"`
	Size       int64  `json:"size,omitempty"`
}

type ThreadResult struct {
	ChannelID   string          `json:"channel_id,omitempty"`
	ChannelName string          `json:"channel_name,omitempty"`
	RootTS      string          `json:"root_ts,omitempty"`
	Permalink   string          `json:"permalink,omitempty"`
	Messages    []MessageResult `json:"messages"`
}

type ChannelResult struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	IsChannel  bool   `json:"is_channel"`
	IsGroup    bool   `json:"is_group"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
	NumMembers int    `json:"num_members,omitempty"`
}

type UserResult struct {
	ID          string `json:"id"`
	TeamID      string `json:"team_id,omitempty"`
	Name        string `json:"name,omitempty"`
	RealName    string `json:"real_name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

type SearchResult struct {
	Query      string          `json:"query"`
	Total      int             `json:"total"`
	Pagination any             `json:"pagination,omitempty"`
	Messages   []MessageResult `json:"messages"`
}

func (client *Client) SearchMessages(ctx context.Context, query string, count int, sort string, includeRichContent bool) (SearchResult, error) {
	values := url.Values{}
	values.Set("query", query)
	values.Set("count", strconv.Itoa(count))
	values.Set("sort", sort)
	values.Set("highlight", "true")
	var response searchMessagesResponse
	if err := client.Call(ctx, "search.messages", values, &response); err != nil {
		return SearchResult{}, err
	}
	messages := make([]MessageResult, 0, len(response.Messages.Matches))
	for index := range response.Messages.Matches {
		messages = append(messages, response.Messages.Matches[index].result(includeRichContent))
	}
	if includeRichContent {
		var err error
		messages, err = client.hydrateSearchMessages(ctx, messages)
		if err != nil {
			return SearchResult{}, err
		}
	}
	return SearchResult{
		Query:      query,
		Total:      response.Messages.Total,
		Pagination: response.Messages.Pagination,
		Messages:   messages,
	}, nil
}

func (client *Client) hydrateSearchMessages(ctx context.Context, messages []MessageResult) ([]MessageResult, error) {
	hydrated := make([]MessageResult, len(messages))
	threadCache := map[string]ThreadResult{}
	messageCache := map[string]MessageResult{}
	for index := range messages {
		searchHit := messages[index]
		message, err := client.hydrateSearchMessage(ctx, searchHit, threadCache, messageCache)
		if err != nil {
			return nil, err
		}
		hydrated[index] = mergeHydratedSearchMessage(searchHit, message)
	}
	return hydrated, nil
}

func (client *Client) hydrateSearchMessage(ctx context.Context, searchHit MessageResult, threadCache map[string]ThreadResult, messageCache map[string]MessageResult) (MessageResult, error) {
	if searchHit.ChannelID == "" || searchHit.TS == "" {
		return searchHit, nil
	}
	rootTS := rootTS(searchHit.TS, searchHit.ThreadTS)
	if rootTS != "" && rootTS != searchHit.TS {
		key := cacheKey(searchHit.ChannelID, rootTS)
		thread, ok := threadCache[key]
		if !ok {
			var err error
			thread, err = client.Thread(ctx, searchHit.ChannelID, rootTS, true)
			if err != nil {
				return MessageResult{}, err
			}
			threadCache[key] = thread
		}
		for index := range thread.Messages {
			if thread.Messages[index].TS == searchHit.TS {
				return thread.Messages[index], nil
			}
		}
		return searchHit, nil
	}
	key := cacheKey(searchHit.ChannelID, searchHit.TS)
	if message, ok := messageCache[key]; ok {
		return message, nil
	}
	message, err := client.MessageAt(ctx, searchHit.ChannelID, searchHit.TS, true)
	if err != nil {
		return MessageResult{}, err
	}
	messageCache[key] = message
	return message, nil
}

func cacheKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func mergeHydratedSearchMessage(searchHit MessageResult, hydrated MessageResult) MessageResult {
	if hydrated.ChannelID == "" {
		hydrated.ChannelID = searchHit.ChannelID
	}
	if hydrated.ChannelName == "" {
		hydrated.ChannelName = searchHit.ChannelName
	}
	if hydrated.TS == "" {
		hydrated.TS = searchHit.TS
	}
	if hydrated.ThreadTS == "" {
		hydrated.ThreadTS = searchHit.ThreadTS
	}
	if hydrated.RootTS == "" {
		hydrated.RootTS = searchHit.RootTS
	}
	if hydrated.Permalink == "" {
		hydrated.Permalink = searchHit.Permalink
	}
	if hydrated.User == "" {
		hydrated.User = searchHit.User
	}
	if hydrated.Username == "" {
		hydrated.Username = searchHit.Username
	}
	if hydrated.Excerpt == "" {
		hydrated.Excerpt = searchHit.Excerpt
	}
	return hydrated
}

func (client *Client) ConversationsList(ctx context.Context, limit int) ([]ChannelResult, error) {
	values := url.Values{}
	values.Set("exclude_archived", "true")
	values.Set("types", "public_channel,private_channel,mpim,im")
	channels := []ChannelResult{}
	cursor := ""
	for {
		pageLimit := 200
		if limit > 0 {
			remaining := limit - len(channels)
			if remaining <= 0 {
				return channels, nil
			}
			pageLimit = min(pageLimit, remaining)
		}
		values.Set("limit", strconv.Itoa(pageLimit))
		if cursor != "" {
			values.Set("cursor", cursor)
		} else {
			values.Del("cursor")
		}
		var response conversationsListResponse
		if err := client.Call(ctx, "conversations.list", values, &response); err != nil {
			return nil, err
		}
		for _, channel := range response.Channels {
			channels = append(channels, channel.result())
			if limit > 0 && len(channels) >= limit {
				return channels, nil
			}
		}
		cursor = response.ResponseMetadata.NextCursor
		if cursor == "" {
			return channels, nil
		}
	}
}

func (client *Client) ConversationInfo(ctx context.Context, channelID string) (ChannelResult, error) {
	values := url.Values{}
	values.Set("channel", channelID)
	var response struct {
		OK      bool         `json:"ok"`
		Channel slackChannel `json:"channel"`
	}
	if err := client.Call(ctx, "conversations.info", values, &response); err != nil {
		return ChannelResult{}, err
	}
	return response.Channel.result(), nil
}

func (client *Client) LookupUserByEmail(ctx context.Context, email string) (UserResult, error) {
	values := url.Values{}
	values.Set("email", email)
	var response usersLookupByEmailResponse
	if err := client.Call(ctx, "users.lookupByEmail", values, &response); err != nil {
		return UserResult{}, err
	}
	return response.User.result(), nil
}

func (client *Client) UserInfo(ctx context.Context, userID string) (UserResult, error) {
	values := url.Values{}
	values.Set("user", userID)
	var response usersLookupByEmailResponse
	if err := client.Call(ctx, "users.info", values, &response); err != nil {
		return UserResult{}, err
	}
	return response.User.result(), nil
}

func (client *Client) UsersList(ctx context.Context, limit int) ([]UserResult, error) {
	if limit <= 0 {
		limit = 200
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(min(limit, 200)))
	users := []UserResult{}
	for {
		var response usersListResponse
		if err := client.Call(ctx, "users.list", values, &response); err != nil {
			return nil, err
		}
		for _, user := range response.Members {
			if user.ID != "" {
				users = append(users, user.result())
			}
			if len(users) >= limit {
				return users, nil
			}
		}
		if response.ResponseMetadata.NextCursor == "" {
			return users, nil
		}
		values.Set("cursor", response.ResponseMetadata.NextCursor)
	}
}

func (client *Client) ConversationHistory(ctx context.Context, channel string, count int, beforeTS string, afterTS string, includeRichContent bool) ([]MessageResult, error) {
	values := url.Values{}
	values.Set("channel", channel)
	if beforeTS != "" {
		values.Set("latest", beforeTS)
	}
	if afterTS != "" {
		values.Set("oldest", afterTS)
	}
	if beforeTS != "" || afterTS != "" {
		values.Set("inclusive", "false")
	}
	if count <= 0 {
		count = 50
	}
	messages := []MessageResult{}
	cursor := ""
	for len(messages) < count {
		values.Set("limit", strconv.Itoa(min(200, count-len(messages))))
		if cursor != "" {
			values.Set("cursor", cursor)
		} else {
			values.Del("cursor")
		}
		var response conversationMessagesResponse
		if err := client.Call(ctx, "conversations.history", values, &response); err != nil {
			return nil, err
		}
		messages = append(messages, messageResults(channel, "", response.Messages, includeRichContent)...)
		cursor = response.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(messages) > count {
		messages = messages[:count]
	}
	return messages, nil
}

func (client *Client) Message(ctx context.Context, channel string, ts string, includeRichContent bool) (MessageResult, error) {
	message, err := client.MessageAt(ctx, channel, ts, includeRichContent)
	if err != nil {
		return MessageResult{}, err
	}
	permalink, _ := client.Permalink(ctx, channel, ts)
	message.Permalink = permalink
	return message, nil
}

func (client *Client) MessageAt(ctx context.Context, channel string, ts string, includeRichContent bool) (MessageResult, error) {
	values := url.Values{}
	values.Set("channel", channel)
	values.Set("latest", ts)
	values.Set("inclusive", "true")
	values.Set("limit", "1")
	var response conversationMessagesResponse
	if err := client.Call(ctx, "conversations.history", values, &response); err != nil {
		return MessageResult{}, err
	}
	messages := messageResults(channel, "", response.Messages, includeRichContent)
	if len(messages) == 0 || messages[0].TS != ts {
		return MessageResult{}, SlackError{Method: "conversations.history", Code: "message_not_found"}
	}
	return messages[0], nil
}

func (client *Client) Thread(ctx context.Context, channel string, ts string, includeRichContent bool) (ThreadResult, error) {
	values := url.Values{}
	values.Set("channel", channel)
	values.Set("ts", ts)
	values.Set("limit", "200")
	messages := []MessageResult{}
	cursor := ""
	for {
		if cursor != "" {
			values.Set("cursor", cursor)
		} else {
			values.Del("cursor")
		}
		var response conversationMessagesResponse
		if err := client.Call(ctx, "conversations.replies", values, &response); err != nil {
			return ThreadResult{}, err
		}
		messages = append(messages, messageResults(channel, "", response.Messages, includeRichContent)...)
		cursor = response.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	rootTS := ts
	if len(messages) > 0 {
		rootTS = messages[0].RootTS
	}
	permalink, _ := client.Permalink(ctx, channel, rootTS)
	return ThreadResult{
		ChannelID: channel,
		RootTS:    rootTS,
		Permalink: permalink,
		Messages:  messages,
	}, nil
}

func (client *Client) Permalink(ctx context.Context, channel string, ts string) (string, error) {
	values := url.Values{}
	values.Set("channel", channel)
	values.Set("message_ts", ts)
	var response struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		Permalink string `json:"permalink"`
	}
	if err := client.Call(ctx, "chat.getPermalink", values, &response); err != nil {
		return "", err
	}
	return response.Permalink, nil
}

type searchMessagesResponse struct {
	OK       bool `json:"ok"`
	Messages struct {
		Total      int             `json:"total"`
		Pagination any             `json:"pagination"`
		Matches    []searchMessage `json:"matches"`
	} `json:"messages"`
}

type searchMessage struct {
	Type        string          `json:"type"`
	User        string          `json:"user"`
	Username    string          `json:"username"`
	Text        string          `json:"text"`
	TS          string          `json:"ts"`
	ThreadTS    string          `json:"thread_ts"`
	Permalink   string          `json:"permalink"`
	Channel     searchChannel   `json:"channel"`
	Blocks      json.RawMessage `json:"blocks"`
	Attachments json.RawMessage `json:"attachments"`
	Files       []slackFile     `json:"files"`
}

type searchChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (message searchMessage) result(includeRichContent bool) MessageResult {
	result := MessageResult{
		ChannelID:   message.Channel.ID,
		ChannelName: message.Channel.Name,
		TS:          message.TS,
		ThreadTS:    message.ThreadTS,
		RootTS:      rootTS(message.TS, message.ThreadTS),
		Permalink:   message.Permalink,
		User:        message.User,
		Username:    message.Username,
		Excerpt:     RichTextFromParts(message.Text, message.Blocks, message.Attachments, fileResults(message.Files)),
	}
	if includeRichContent {
		result.Blocks = message.Blocks
		result.Attachments = message.Attachments
		result.Files = fileResults(message.Files)
	}
	return result
}

type conversationsListResponse struct {
	OK               bool             `json:"ok"`
	Channels         []slackChannel   `json:"channels"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

type slackChannel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsChannel  bool   `json:"is_channel"`
	IsGroup    bool   `json:"is_group"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
	NumMembers int    `json:"num_members"`
}

func (channel slackChannel) result() ChannelResult {
	return ChannelResult(channel)
}

type usersLookupByEmailResponse struct {
	OK   bool      `json:"ok"`
	User slackUser `json:"user"`
}

type usersListResponse struct {
	OK               bool             `json:"ok"`
	Members          []slackUser      `json:"members"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

type slackUser struct {
	ID       string       `json:"id"`
	TeamID   string       `json:"team_id"`
	Name     string       `json:"name"`
	RealName string       `json:"real_name"`
	Profile  slackProfile `json:"profile"`
}

type slackProfile struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	RealName    string `json:"real_name"`
}

func (user slackUser) result() UserResult {
	realName := user.RealName
	if realName == "" {
		realName = user.Profile.RealName
	}
	return UserResult{
		ID:          user.ID,
		TeamID:      user.TeamID,
		Name:        user.Name,
		RealName:    realName,
		DisplayName: user.Profile.DisplayName,
		Email:       user.Profile.Email,
	}
}

type conversationMessagesResponse struct {
	OK               bool             `json:"ok"`
	Messages         []slackMessage   `json:"messages"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

type slackMessage struct {
	Type        string          `json:"type"`
	User        string          `json:"user"`
	Username    string          `json:"username"`
	Text        string          `json:"text"`
	TS          string          `json:"ts"`
	ThreadTS    string          `json:"thread_ts"`
	Blocks      json.RawMessage `json:"blocks"`
	Attachments json.RawMessage `json:"attachments"`
	Files       []slackFile     `json:"files"`
}

type slackFile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Title      string `json:"title"`
	Mimetype   string `json:"mimetype"`
	Filetype   string `json:"filetype"`
	URLPrivate string `json:"url_private"`
	Permalink  string `json:"permalink"`
	Size       int64  `json:"size"`
}

func (file slackFile) result() FileResult {
	return FileResult(file)
}

func fileResults(files []slackFile) []FileResult {
	if len(files) == 0 {
		return nil
	}
	results := make([]FileResult, 0, len(files))
	for index := range files {
		results = append(results, files[index].result())
	}
	return results
}

func messageResults(channelID string, channelName string, messages []slackMessage, includeRichContent bool) []MessageResult {
	results := make([]MessageResult, 0, len(messages))
	for index := range messages {
		message := messages[index]
		result := MessageResult{
			ChannelID:   channelID,
			ChannelName: channelName,
			TS:          message.TS,
			ThreadTS:    message.ThreadTS,
			RootTS:      rootTS(message.TS, message.ThreadTS),
			User:        message.User,
			Username:    message.Username,
			Excerpt:     RichTextFromParts(message.Text, message.Blocks, message.Attachments, fileResults(message.Files)),
		}
		if includeRichContent {
			result.TextFull = message.Text
			result.Blocks = message.Blocks
			result.Attachments = message.Attachments
			result.Files = fileResults(message.Files)
		}
		results = append(results, result)
	}
	return results
}

func rootTS(ts string, threadTS string) string {
	if threadTS != "" {
		return threadTS
	}
	return ts
}

func CleanSlackText(text string) string {
	text = strings.NewReplacer("\uE000", "", "\uE001", "").Replace(text)
	text = cleanSlackMarkup(text)
	return strings.Join(strings.Fields(text), " ")
}

func cleanSlackMarkup(text string) string {
	text = html.UnescapeString(text)
	text = channelMentionTokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		matches := channelMentionTokenPattern.FindStringSubmatch(token)
		if len(matches) >= 3 && matches[2] != "" {
			return "#" + matches[2]
		}
		if len(matches) >= 2 {
			return "#" + matches[1]
		}
		return token
	})
	text = userMentionTokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		matches := userMentionTokenPattern.FindStringSubmatch(token)
		if len(matches) >= 3 && matches[2] != "" {
			return "@" + matches[2]
		}
		if len(matches) >= 2 {
			return "@" + matches[1]
		}
		return token
	})
	text = labeledLinkTokenPattern.ReplaceAllString(text, "$2 ($1)")
	text = bareLinkTokenPattern.ReplaceAllString(text, "$1")
	text = specialSlackTokenPattern.ReplaceAllStringFunc(text, func(token string) string {
		matches := specialSlackTokenPattern.FindStringSubmatch(token)
		if len(matches) >= 3 && matches[2] != "" {
			return matches[2]
		}
		if len(matches) >= 2 {
			return matches[1]
		}
		return token
	})
	return strings.NewReplacer("<", " ", ">", " ", "&", " ").Replace(text)
}
