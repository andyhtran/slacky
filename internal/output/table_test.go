package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableRendererBoundsRowsAtRequestedWidths(t *testing.T) {
	for _, width := range []int{80, 100} {
		lines := TableLines(Table{
			Width:  width,
			Indent: "  ",
			Columns: []TableColumn{
				{Header: "NAME", MinWidth: 4, MaxWidth: 12},
				{Header: "DESCRIPTION", MinWidth: 20, Flex: true},
			},
			Rows: [][]string{{"core", strings.Repeat("long description ", 12)}},
		})
		for _, line := range lines {
			if got := VisibleWidth(line); got > width {
				t.Fatalf("line visible width = %d, want <= %d\n%s", got, width, line)
			}
		}
		if !strings.Contains(strings.Join(lines, "\n"), "...") {
			t.Fatalf("expected long table cell to be truncated:\n%s", strings.Join(lines, "\n"))
		}
	}
}

func TestTableRendererStylesHeaderAndSeparator(t *testing.T) {
	SetColor(true)
	defer SetColor(false)

	lines := TableLines(Table{
		Width:  40,
		Styled: true,
		Columns: []TableColumn{
			{Header: "NAME", Width: 4},
			{Header: "STATUS", Width: 6},
		},
		Rows: [][]string{{"work", Green("ready")}},
	})
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	for _, line := range lines[:2] {
		if !strings.HasPrefix(line, ansiDim) || !strings.HasSuffix(line, ansiReset) {
			t.Fatalf("header/separator should be dim styled, got %q", line)
		}
	}
	if got := VisibleWidth(lines[2]); got > 40 {
		t.Fatalf("styled row visible width = %d, want <= 40", got)
	}
}

func TestTableRendererNoColorEnvironmentSuppressesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	SetColor(initColor())
	defer SetColor(false)

	text := strings.Join(TableLines(Table{
		Width:  40,
		Styled: true,
		Columns: []TableColumn{
			{Header: "NAME", Width: 4},
			{Header: "STATUS", Width: 6},
		},
		Rows: [][]string{{"work", "ready"}},
	}), "\n")
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("NO_COLOR table output should not contain ANSI: %q", text)
	}
}

func TestWriteTableWritesToBuilder(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTable(&buf, Table{
		Width: 40,
		Columns: []TableColumn{
			{Header: "NAME", Width: 4},
			{Header: "STATUS", Width: 6},
		},
		Rows: [][]string{{"work", "ready"}},
	}); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "NAME") || !strings.Contains(text, "ready") {
		t.Fatalf("table output missing expected cells:\n%s", text)
	}
}

func TestVisibleWidthIgnoresANSI(t *testing.T) {
	SetColor(true)
	defer SetColor(false)

	value := Dim("NAME") + "  " + Green("ready")
	if got := VisibleWidth(value); got != len("NAME  ready") {
		t.Fatalf("VisibleWidth = %d", got)
	}
	if stripped := StripANSI(value); stripped != "NAME  ready" {
		t.Fatalf("StripANSI = %q", stripped)
	}
}
