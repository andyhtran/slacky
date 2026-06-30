package app

import (
	"errors"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/store"
)

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
