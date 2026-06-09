package resolve

import (
	"fmt"
	"net/url"
	"strings"
)

type SlackRef struct {
	URL       string `json:"url"`
	TeamHost  string `json:"team_host,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	TS        string `json:"ts,omitempty"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

func ParseArchiveURL(raw string) (SlackRef, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return SlackRef{}, err
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "archives" {
		return SlackRef{}, fmt.Errorf("expected Slack archive URL like https://workspace.slack.com/archives/C123/p1717440000000000")
	}
	ts, err := parsePackedTimestamp(parts[2])
	if err != nil {
		return SlackRef{}, err
	}
	ref := SlackRef{
		URL:       raw,
		TeamHost:  parsed.Host,
		ChannelID: parts[1],
		TS:        ts,
	}
	if threadTS := strings.TrimSpace(parsed.Query().Get("thread_ts")); threadTS != "" {
		ref.ThreadTS = threadTS
	}
	return ref, nil
}

func parsePackedTimestamp(value string) (string, error) {
	if !strings.HasPrefix(value, "p") {
		return "", fmt.Errorf("expected Slack packed timestamp segment starting with p")
	}
	digits := strings.TrimPrefix(value, "p")
	if len(digits) < 7 {
		return "", fmt.Errorf("expected packed timestamp with seconds and microseconds")
	}
	seconds := digits[:len(digits)-6]
	micros := digits[len(digits)-6:]
	return seconds + "." + micros, nil
}
