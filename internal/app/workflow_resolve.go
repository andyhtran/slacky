package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/store"
)

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
