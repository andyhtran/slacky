package resolve

import "testing"

func TestParseArchiveURLWithThreadTimestamp(t *testing.T) {
	ref, err := ParseArchiveURL("https://example.slack.com/archives/C123/p1780951295323189?thread_ts=1780951295.096439&cid=C123")
	if err != nil {
		t.Fatalf("parse archive URL: %v", err)
	}
	if ref.ChannelID != "C123" || ref.TS != "1780951295.323189" || ref.ThreadTS != "1780951295.096439" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
}
