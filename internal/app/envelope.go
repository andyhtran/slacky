package app

import (
	"os"

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
		return output.WriteJSON(os.Stdout, envelope)
	}
	return output.WriteText(os.Stdout, envelope.Text)
}
