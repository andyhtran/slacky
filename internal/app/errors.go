package app

import (
	"errors"
	"fmt"
	"strings"
)

type AppError struct {
	Code              int      `json:"-"`
	Kind              string   `json:"kind"`
	Message           string   `json:"message"`
	Usage             string   `json:"usage,omitempty"`
	Examples          []string `json:"examples,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
}

func (err *AppError) Error() string {
	if err == nil {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(err.Message)
	if err.Usage != "" {
		builder.WriteString("\n\nUsage: ")
		builder.WriteString(err.Usage)
	}
	if len(err.Examples) > 0 {
		builder.WriteString("\n\nTry:")
		for _, example := range err.Examples {
			builder.WriteString("\n  ")
			builder.WriteString(example)
		}
	}
	return builder.String()
}

func appError(kind, message string) *AppError {
	return &AppError{
		Code:    2,
		Kind:    kind,
		Message: message,
	}
}

func missingUsage(message, usage string, examples ...string) *AppError {
	err := appError("usage", message)
	err.Usage = usage
	err.Examples = examples
	return err
}

func missingAuthError(path string) *AppError {
	err := appError("missing_auth", fmt.Sprintf("missing Slack user token at %s", path))
	err.Examples = []string{
		"slacky setup wizard",
		"slacky setup manifest",
		"slacky auth login",
		"slacky auth import",
		"slacky search --local \"release notes\"",
	}
	err.SuggestedCommands = []string{"slacky auth status", "slacky setup wizard", "slacky auth import"}
	return err
}

func exitCode(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) && appErr.Code != 0 {
		return appErr.Code
	}
	return 1
}
