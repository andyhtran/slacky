package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiCyan  = "\x1b[36m"
)

var colorEnabled = initColor()

func initColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTTY(os.Stdout)
}

func SetColor(enabled bool) {
	colorEnabled = enabled
}

func IsTTY(file *os.File) bool {
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func WriteText(w io.Writer, text string) error {
	if strings.HasSuffix(text, "\n") {
		_, err := io.WriteString(w, text)
		return err
	}
	_, err := fmt.Fprintln(w, text)
	return err
}

func TerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 100
	}
	return width
}

func Bold(value string) string {
	if !colorEnabled {
		return value
	}
	return ansiBold + value + ansiReset
}

func Dim(value string) string {
	if !colorEnabled {
		return value
	}
	return ansiDim + value + ansiReset
}

func Cyan(value string) string {
	if !colorEnabled {
		return value
	}
	return ansiCyan + value + ansiReset
}

func HighlightTerms(value string, terms []string) string {
	if !colorEnabled || len(terms) == 0 {
		return value
	}
	terms = normalizedHighlightTerms(terms)
	if len(terms) == 0 {
		return Dim(value)
	}
	lower := strings.ToLower(value)
	var builder strings.Builder
	cursor := 0
	found := false
	for cursor < len(value) {
		nextStart := -1
		nextTerm := ""
		for _, term := range terms {
			if index := strings.Index(lower[cursor:], term); index >= 0 {
				absolute := cursor + index
				if nextStart < 0 || absolute < nextStart || absolute == nextStart && len(term) > len(nextTerm) {
					nextStart = absolute
					nextTerm = term
				}
			}
		}
		if nextStart < 0 {
			builder.WriteString(Dim(value[cursor:]))
			break
		}
		if nextStart > cursor {
			builder.WriteString(Dim(value[cursor:nextStart]))
		}
		end := nextStart + len(nextTerm)
		builder.WriteString(Bold(value[nextStart:end]))
		cursor = end
		found = true
	}
	if !found {
		return Dim(value)
	}
	return builder.String()
}

func normalizedHighlightTerms(terms []string) []string {
	seen := map[string]bool{}
	normalized := []string{}
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		term = strings.Trim(term, "\"'`.,:;()[]{}<>")
		if len(term) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		normalized = append(normalized, term)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if len(normalized[i]) == len(normalized[j]) {
			return normalized[i] < normalized[j]
		}
		return len(normalized[i]) > len(normalized[j])
	})
	return normalized
}

func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = singleLine(value)
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func Wrap(value string, width int) []string {
	if width <= 0 {
		return []string{strings.TrimSpace(value)}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		for len([]rune(word)) > width {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		if current == "" {
			current = word
			continue
		}
		if len([]rune(current))+1+len([]rune(word)) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		current = word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func singleLine(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), " ")
}
