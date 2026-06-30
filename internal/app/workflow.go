package app

import (
	"regexp"
	"strings"
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
