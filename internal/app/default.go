package app

import (
	"fmt"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/paths"
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

	lines := make([]string, 0, 16)
	lines = append(
		lines,
		"slacky",
		"",
		fmt.Sprintf("Home: %s", pathSet.Home.Path),
		fmt.Sprintf("Auth: %s", authReadinessLabel(authStatus.ReadyForSlack, authUser)),
		fmt.Sprintf("Cache: %s", cacheLabel(cacheStatus)),
		"",
		"More:",
		"  slacky paths",
		"  slacky auth status",
		"  slacky cache status",
		"",
	)
	lines = append(lines, defaultNextLines(authStatus.ReadyForSlack, authUser)...)
	text := strings.Join(lines, "\n")

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

func defaultNextLines(authReady bool, authUser string) []string {
	if !authReady {
		return []string{
			"Next:",
			"  slacky setup wizard",
			"  slacky auth import",
		}
	}
	return []string{
		"Next:",
		"  " + defaultSearchCommand(authUser),
		"  slacky channels",
		"  slacky history --channel general --count 25",
	}
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

func cacheLabel(status store.Status) string {
	if status.Exists {
		return fmt.Sprintf("%s at %s", humanByteSize(status.SizeBytes), status.CachePath)
	}
	return "not created"
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
