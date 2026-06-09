package store

import (
	"database/sql"
	"encoding/json"
	"html"
	"regexp"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
)

var (
	userMentionTokenPattern    = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]+))?>`)
	channelMentionTokenPattern = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]+))?>`)
	labeledLinkTokenPattern    = regexp.MustCompile(`<((?:https?|mailto):[^>|]+)\|([^>]+)>`)
	bareLinkTokenPattern       = regexp.MustCompile(`<((?:https?|mailto):[^>]+)>`)
	specialSlackTokenPattern   = regexp.MustCompile(`<!([^>|]+)(?:\|([^>]+))?>`)
)

func normalizeMessageText(message api.MessageResult) string {
	parts := []string{}
	seen := map[string]bool{}
	appendNormalizedPart(&parts, seen, message.TextFull)
	appendNormalizedPart(&parts, seen, message.Excerpt)
	appendNormalizedPart(&parts, seen, api.RichTextFromParts("", message.Blocks, message.Attachments, message.Files))
	return strings.Join(parts, " ")
}

func appendNormalizedPart(parts *[]string, seen map[string]bool, value string) {
	normalized := normalizeWhitespace(cleanSlackMarkup(value))
	if normalized == "" || seen[normalized] {
		return
	}
	seen[normalized] = true
	*parts = append(*parts, normalized)
}

func cleanSlackMarkup(value string) string {
	value = html.UnescapeString(value)
	value = channelMentionTokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		matches := channelMentionTokenPattern.FindStringSubmatch(token)
		if len(matches) < 3 {
			return token
		}
		if matches[2] != "" {
			return "#" + matches[2] + " " + matches[1]
		}
		return "#" + matches[1]
	})
	value = userMentionTokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		matches := userMentionTokenPattern.FindStringSubmatch(token)
		if len(matches) < 3 {
			return token
		}
		if matches[2] != "" {
			return "@" + matches[2] + " " + matches[1]
		}
		return "@" + matches[1]
	})
	value = labeledLinkTokenPattern.ReplaceAllString(value, "$2 $1")
	value = bareLinkTokenPattern.ReplaceAllString(value, "$1")
	value = specialSlackTokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		matches := specialSlackTokenPattern.FindStringSubmatch(token)
		if len(matches) >= 3 && matches[2] != "" {
			return matches[2]
		}
		if len(matches) >= 2 {
			return matches[1]
		}
		return token
	})
	value = strings.NewReplacer("<", " ", ">", " ", "&", " ").Replace(value)
	return normalizeWhitespace(value)
}

func extractMentions(message api.MessageResult) []MessageMention {
	parts := make([]string, 0, 4)
	parts = append(
		parts,
		message.TextFull,
		message.Excerpt,
		rawString(message.Blocks),
		rawString(message.Attachments),
	)
	parts = append(parts, api.VisibleJSONText(message.Blocks)...)
	parts = append(parts, api.VisibleJSONText(message.Attachments)...)
	mentions := extractTokenMentions(strings.Join(parts, " "))
	mentions = append(mentions, extractStructuredMentions(message.Blocks)...)
	mentions = append(mentions, extractStructuredMentions(message.Attachments)...)
	return dedupeMentions(mentions)
}

func extractTokenMentions(value string) []MessageMention {
	mentions := []MessageMention{}
	for _, matches := range userMentionTokenPattern.FindAllStringSubmatch(value, -1) {
		if len(matches) >= 2 {
			mentions = append(mentions, MessageMention{Type: "user", TargetID: matches[1], DisplayText: mentionDisplay(matches)})
		}
	}
	for _, matches := range channelMentionTokenPattern.FindAllStringSubmatch(value, -1) {
		if len(matches) >= 2 {
			mentions = append(mentions, MessageMention{Type: "channel", TargetID: matches[1], DisplayText: mentionDisplay(matches)})
		}
	}
	return mentions
}

func extractStructuredMentions(raw json.RawMessage) []MessageMention {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	mentions := []MessageMention{}
	walkStructuredMentions(value, &mentions)
	return mentions
}

func walkStructuredMentions(value any, mentions *[]MessageMention) {
	switch typed := value.(type) {
	case map[string]any:
		if id, ok := stringValue(typed, "user_id", "user"); ok && looksUserID(id) {
			*mentions = append(*mentions, MessageMention{Type: "user", TargetID: id})
		}
		if id, ok := stringValue(typed, "channel_id", "channel"); ok && looksMentionChannelID(id) {
			*mentions = append(*mentions, MessageMention{Type: "channel", TargetID: id})
		}
		for _, childValue := range typed {
			walkStructuredMentions(childValue, mentions)
		}
	case []any:
		for _, childValue := range typed {
			walkStructuredMentions(childValue, mentions)
		}
	}
}

func stringValue(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func looksUserID(value string) bool {
	return len(value) > 1 && (strings.HasPrefix(value, "U") || strings.HasPrefix(value, "W"))
}

func looksMentionChannelID(value string) bool {
	return len(value) > 1 && (strings.HasPrefix(value, "C") || strings.HasPrefix(value, "G") || strings.HasPrefix(value, "D"))
}

func mentionDisplay(matches []string) string {
	if len(matches) >= 3 {
		return cleanSlackMarkup(matches[2])
	}
	return ""
}

func dedupeMentions(mentions []MessageMention) []MessageMention {
	seen := map[string]bool{}
	deduped := make([]MessageMention, 0, len(mentions))
	for _, mention := range mentions {
		if mention.Type == "" || mention.TargetID == "" {
			continue
		}
		key := mention.Type + "\x00" + mention.TargetID
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, mention)
	}
	return deduped
}

func replaceMessageMentions(tx *sql.Tx, channelID string, ts string, mentions []MessageMention) error {
	if _, err := tx.Exec(`DELETE FROM message_mentions WHERE channel_id = ? AND ts = ?`, channelID, ts); err != nil {
		return err
	}
	for _, mention := range mentions {
		if _, err := tx.Exec(
			`INSERT INTO message_mentions(channel_id, ts, mention_type, target_id, display_text, updated_at)
			 VALUES(?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(channel_id, ts, mention_type, target_id) DO UPDATE SET
			   display_text=excluded.display_text,
			   updated_at=CURRENT_TIMESTAMP`,
			channelID, ts, mention.Type, mention.TargetID, mention.DisplayText,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
