package app

import (
	"fmt"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/skill"
	"github.com/andyhtran/slacky/internal/store"
)

type DefaultCmd struct{}

type VersionCmd struct{}

func (cmd *DefaultCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	authStatus := inspectAuthStatus(pathSet)
	cacheStatus := store.Inspect(pathSet.CacheDB.Path)
	authUser := authenticatedUserLabel(authStatus, pathSet.CacheDB.Path)

	text := defaultDashboardText(authStatus, cacheStatus, authUser, !globals.JSON && !globals.Raw && !globals.NoColor)

	return writeEnvelope(globals, Envelope{
		OK:      true,
		Text:    text,
		Source:  "cache",
		Paths:   pathSet,
		Auth:    authStatus,
		Cache:   cacheStatus,
		Results: []any{},
	})
}

func (cmd *VersionCmd) Run(globals *Globals) error {
	text := fmt.Sprintf("slacky %s", appVersion)
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Version: map[string]string{
			"version": appVersion,
		},
	})
}

func defaultDashboardText(authStatus config.AuthStatus, cacheStatus store.Status, authUser string, styled bool) string {
	nextLines := defaultNextLines(authStatus.ReadyForSlack, authUser)
	lines := make([]string, 0, 11+len(nextLines))
	lines = append(
		lines,
		styleIf(styled, output.Bold, "slacky"),
		appDescription,
		"",
		fmt.Sprintf("Auth: %s", authReadinessLabel(authStatus.ReadyForSlack, authUser)),
		fmt.Sprintf("Cache: %s", defaultCacheLabel(cacheStatus)),
		"",
		"Usage:",
		"  slacky <command> [options]",
		"Start here (for AI agents):",
		"  slacky skills get core",
		"",
	)
	lines = append(lines, nextLines...)
	return strings.Join(styleGuidanceLines(lines, styled), "\n")
}

func defaultNextLines(authReady bool, authUser string) []string {
	if !authReady {
		return appendRuntimeSkillList([]string{
			"Next:",
			"  slacky auth status",
			"  slacky setup wizard",
			"  slacky auth import",
		})
	}
	return appendRuntimeSkillList([]string{
		"Next:",
		"  slacky auth status",
		"  " + defaultSearchCommand(authUser),
		"  slacky channels",
		"  slacky search --local " + shellQuote("topic words"),
	})
}

func appendRuntimeSkillList(lines []string) []string {
	visible := 0
	for _, guide := range skill.ListGuides() {
		if guide.Visible {
			visible++
		}
	}
	if visible > 1 {
		lines = append(lines, "  slacky skills list")
	}
	return lines
}

func styleGuidanceLines(lines []string, styled bool) []string {
	styledLines := make([]string, len(lines))
	section := ""
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Usage:" || trimmed == "Start here (for AI agents):" || trimmed == "Next:":
			section = trimmed
			styledLines[index] = styleIf(styled, output.Dim, line)
		case trimmed == "":
			styledLines[index] = line
		case strings.HasPrefix(line, "  ") && section != "Usage:":
			styledLines[index] = "  " + styleIf(styled, output.Cyan, trimmed)
		default:
			styledLines[index] = line
		}
	}
	return styledLines
}

func styleIf(enabled bool, apply func(string) string, value string) string {
	if !enabled {
		return value
	}
	return apply(value)
}

func defaultSearchCommand(authUser string) string {
	if strings.TrimSpace(authUser) == "" {
		authUser = "@someone"
	}
	return fmt.Sprintf("slacky search %s", shellQuote("from:"+authUser+" has:link"))
}

func authenticatedUserLabel(authStatus config.AuthStatus, cachePath string) string {
	if !authStatus.ReadyForSlack {
		return ""
	}
	if label := authMentionLabel(authStatus.UserID, authStatus.UserName); label != "" && !looksUserID(strings.TrimPrefix(label, "@")) {
		return label
	}
	if authStatus.UserID != "" {
		if cacheDB, err := store.OpenReadOnly(cachePath); err == nil {
			defer func() {
				_ = cacheDB.Close()
			}()
			if user, found, userErr := cacheDB.UserByID(authStatus.UserID); userErr == nil && found {
				if label := authUserMentionLabel(user); label != "" {
					return label
				}
			}
		}
	}
	return authMentionLabel(authStatus.UserID, authStatus.UserName)
}

func authUserMentionLabel(user api.UserResult) string {
	return authMentionLabel(user.ID, firstNonEmpty(user.Name, user.DisplayName, user.RealName))
}

func authMentionLabel(userID string, userName string) string {
	label := strings.TrimSpace(userName)
	label = strings.TrimPrefix(label, "@")
	if label != "" {
		if looksUserID(label) {
			return firstNonEmpty(strings.TrimSpace(userID), label)
		}
		return "@" + label
	}
	if strings.TrimSpace(userID) != "" {
		return userID
	}
	return ""
}

func authDisplayLabel(userID string, userName string) string {
	label := authMentionLabel(userID, userName)
	if label == "" {
		return blank("")
	}
	if userID != "" && strings.HasPrefix(label, "@") {
		return fmt.Sprintf("%s (%s)", label, userID)
	}
	return label
}

func authReadinessLabel(ready bool, authUser string) string {
	label := readiness(ready)
	if ready && strings.TrimSpace(authUser) != "" {
		return label + " as " + authUser
	}
	return label
}

func readiness(ready bool) string {
	if ready {
		return "ready"
	}
	return "missing user token"
}

func defaultCacheLabel(status store.Status) string {
	if !status.Exists {
		return "not created"
	}
	if !status.Openable {
		return "not openable"
	}
	parts := []string{}
	for _, item := range []struct {
		key      string
		singular string
	}{
		{key: "messages", singular: "message"},
		{key: "channels", singular: "channel"},
		{key: "users", singular: "user"},
	} {
		if count := status.Counts[item.key]; count > 0 {
			parts = append(parts, pluralCount(count, item.singular))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("empty (%s)", humanByteSize(status.SizeBytes))
	}
	return fmt.Sprintf("%s (%s)", strings.Join(parts, ", "), humanByteSize(status.SizeBytes))
}

func cacheLabel(status store.Status) string {
	if status.Exists {
		return fmt.Sprintf("%s at %s", humanByteSize(status.SizeBytes), status.CachePath)
	}
	return "not created"
}

func pluralCount(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func humanByteSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 {
			if value == float64(int64(value)) {
				return fmt.Sprintf("%.0f %s", value, unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", value/1024)
}

func humanByteSizeWithExact(size int64) string {
	return fmt.Sprintf("%s (%s bytes)", humanByteSize(size), groupedInt(size))
}

func groupedInt(value int64) string {
	if value == 0 {
		return "0"
	}
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	digits := fmt.Sprintf("%d", value)
	firstGroup := len(digits) % 3
	if firstGroup == 0 {
		firstGroup = 3
	}
	groups := []string{digits[:firstGroup]}
	for i := firstGroup; i < len(digits); i += 3 {
		groups = append(groups, digits[i:i+3])
	}
	return sign + strings.Join(groups, ",")
}
