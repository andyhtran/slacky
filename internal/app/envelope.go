package app

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/output"
)

type Envelope struct {
	OK           bool   `json:"ok"`
	Text         string `json:"text,omitempty"`
	Source       string `json:"source,omitempty"`
	CacheNotice  string `json:"cache_notice,omitempty"`
	Results      any    `json:"results,omitempty"`
	Search       any    `json:"search,omitempty"`
	Message      any    `json:"message,omitempty"`
	Thread       any    `json:"thread,omitempty"`
	Threads      any    `json:"threads,omitempty"`
	Channels     any    `json:"channels,omitempty"`
	User         any    `json:"user,omitempty"`
	Cache        any    `json:"cache,omitempty"`
	Auth         any    `json:"auth,omitempty"`
	Paths        any    `json:"paths,omitempty"`
	Version      any    `json:"version,omitempty"`
	Schema       any    `json:"schema,omitempty"`
	AgentContext any    `json:"agent_context,omitempty"`
	Skill        any    `json:"skill,omitempty"`
	Skills       any    `json:"skills,omitempty"`
	Commands     any    `json:"commands,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

type ErrorEnvelope struct {
	OK    bool      `json:"ok"`
	Error *AppError `json:"error"`
}

func writeEnvelope(globals *Globals, envelope Envelope) error {
	if globals.JSON {
		envelope = enrichEnvelopeForJSON(envelope)
		return output.WriteJSON(os.Stdout, envelope)
	}
	return output.WriteText(os.Stdout, envelope.Text)
}

func enrichEnvelopeForJSON(envelope Envelope) Envelope {
	envelope.Results = enrichJSONValue(envelope.Results)
	envelope.Message = enrichJSONValue(envelope.Message)
	envelope.Thread = enrichJSONValue(envelope.Thread)
	envelope.Threads = enrichJSONValue(envelope.Threads)
	return envelope
}

func enrichJSONValue(value any) any {
	switch typed := value.(type) {
	case api.MessageResult:
		return enrichMessageForJSON(typed)
	case []api.MessageResult:
		return enrichMessagesForJSON(typed)
	case api.ThreadResult:
		return enrichThreadForJSON(typed)
	case []api.ThreadResult:
		return enrichThreadsForJSON(typed)
	default:
		return value
	}
}

func enrichMessagesForJSON(messages []api.MessageResult) []api.MessageResult {
	enriched := make([]api.MessageResult, len(messages))
	for index := range messages {
		enriched[index] = enrichMessageForJSON(messages[index])
	}
	return enriched
}

func enrichThreadsForJSON(threads []api.ThreadResult) []api.ThreadResult {
	enriched := make([]api.ThreadResult, len(threads))
	for index := range threads {
		enriched[index] = enrichThreadForJSON(threads[index])
	}
	return enriched
}

func enrichMessageForJSON(message api.MessageResult) api.MessageResult {
	if message.DisplayName == "" {
		message.DisplayName = strings.TrimPrefix(strings.TrimSpace(message.Username), "@")
	}
	datetime, date := slackTimestampFields(message.TS)
	if message.Datetime == "" {
		message.Datetime = datetime
	}
	if message.Date == "" {
		message.Date = date
	}
	return message
}

func enrichThreadForJSON(thread api.ThreadResult) api.ThreadResult {
	thread.Messages = enrichMessagesForJSON(thread.Messages)
	if thread.ChannelName == "" {
		for index := range thread.Messages {
			if thread.Messages[index].ChannelName != "" {
				thread.ChannelName = thread.Messages[index].ChannelName
				break
			}
		}
	}
	datetime, date := slackTimestampFields(thread.RootTS)
	if thread.Datetime == "" {
		thread.Datetime = datetime
	}
	if thread.Date == "" {
		thread.Date = date
	}
	return thread
}

func slackTimestampFields(ts string) (string, string) {
	instant, ok := slackTimestampTime(ts)
	if !ok {
		return "", ""
	}
	return instant.Format(time.RFC3339Nano), instant.Format("2006-01-02")
}

func slackTimestampTime(ts string) (time.Time, bool) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, false
	}
	secondsText, fractionText, _ := strings.Cut(ts, ".")
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	fractionText = strings.TrimSpace(fractionText)
	if len(fractionText) > 9 {
		fractionText = fractionText[:9]
	}
	for len(fractionText) < 9 {
		fractionText += "0"
	}
	var nanos int64
	if fractionText != "" {
		nanos, err = strconv.ParseInt(fractionText, 10, 64)
		if err != nil {
			return time.Time{}, false
		}
	}
	return time.Unix(seconds, nanos).UTC(), true
}
