package output

import (
	"io"
	"strings"
	"unicode/utf8"
)

type TableColumn struct {
	Header   string
	Width    int
	MinWidth int
	MaxWidth int
	Flex     bool
}

type Table struct {
	Columns     []TableColumn
	Rows        [][]string
	Width       int
	Indent      string
	Gutter      string
	Styled      bool
	NoSeparator bool
}

func WriteTable(w io.Writer, table Table) error {
	lines := TableLines(table)
	for index, line := range lines {
		if index > 0 {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}

func TableLines(table Table) []string {
	if len(table.Columns) == 0 {
		return nil
	}
	widths := tableColumnWidths(table)
	lines := []string{}
	header := renderTableRow(table, tableHeaders(table.Columns), widths)
	lines = append(lines, styleTableGuide(table.Styled, header))
	if !table.NoSeparator {
		separator := make([]string, len(widths))
		for index, width := range widths {
			separator[index] = strings.Repeat("-", width)
		}
		lines = append(lines, styleTableGuide(table.Styled, renderTableRow(table, separator, widths)))
	}
	for _, row := range table.Rows {
		lines = append(lines, renderTableRow(table, row, widths))
	}
	return lines
}

func StripANSI(value string) string {
	var builder strings.Builder
	for index := 0; index < len(value); {
		if value[index] == '\x1b' {
			index = skipANSISequence(value, index)
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		builder.WriteRune(r)
		index += size
	}
	return builder.String()
}

func VisibleWidth(value string) int {
	return len([]rune(StripANSI(value)))
}

func TruncateVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	normalized := singleLine(value)
	plain := []rune(singleLine(StripANSI(normalized)))
	if len(plain) <= width {
		return normalized
	}
	if width <= 3 {
		return string(plain[:width])
	}
	return string(plain[:width-3]) + "..."
}

func tableColumnWidths(table Table) []int {
	widths := make([]int, len(table.Columns))
	minWidths := make([]int, len(table.Columns))
	for index, column := range table.Columns {
		minWidth := column.MinWidth
		if minWidth <= 0 {
			minWidth = VisibleWidth(column.Header)
		}
		if minWidth <= 0 {
			minWidth = 1
		}
		if column.Width > 0 {
			widths[index] = column.Width
			if widths[index] < 1 {
				widths[index] = 1
			}
			minWidths[index] = widths[index]
			continue
		}
		desired := VisibleWidth(column.Header)
		for _, row := range table.Rows {
			if index < len(row) && VisibleWidth(row[index]) > desired {
				desired = VisibleWidth(row[index])
			}
		}
		if column.MaxWidth > 0 && desired > column.MaxWidth {
			desired = column.MaxWidth
		}
		if desired < minWidth {
			desired = minWidth
		}
		widths[index] = desired
		minWidths[index] = minWidth
	}

	available := table.Width
	if available <= 0 {
		available = TerminalWidth()
	}
	available -= VisibleWidth(table.Indent) + VisibleWidth(tableGutter(table))*(len(table.Columns)-1)
	if available < len(table.Columns) {
		available = len(table.Columns)
	}

	if total := sumInts(widths); total > available {
		shrinkTableWidths(widths, minWidths, total-available)
	}
	if total := sumInts(widths); total < available {
		growTableFlexColumns(table.Columns, widths, available-total, available)
	}
	return widths
}

func tableHeaders(columns []TableColumn) []string {
	headers := make([]string, len(columns))
	for index, column := range columns {
		headers[index] = column.Header
	}
	return headers
}

func renderTableRow(table Table, cells []string, widths []int) string {
	parts := make([]string, len(widths))
	for index, width := range widths {
		cell := ""
		if index < len(cells) {
			cell = cells[index]
		}
		parts[index] = renderTableCell(cell, width)
	}
	return strings.TrimRight(table.Indent+strings.Join(parts, tableGutter(table)), " ")
}

func renderTableCell(value string, width int) string {
	value = TruncateVisible(value, width)
	padding := width - VisibleWidth(value)
	if padding < 0 {
		padding = 0
	}
	return value + strings.Repeat(" ", padding)
}

func tableGutter(table Table) string {
	if table.Gutter == "" {
		return "  "
	}
	return table.Gutter
}

func styleTableGuide(styled bool, value string) string {
	if !styled {
		return value
	}
	return Dim(value)
}

func shrinkTableWidths(widths []int, minWidths []int, excess int) {
	for excess > 0 {
		changed := false
		for index := len(widths) - 1; index >= 0 && excess > 0; index-- {
			floor := minWidths[index]
			if floor < 1 {
				floor = 1
			}
			if widths[index] <= floor {
				continue
			}
			delta := widths[index] - floor
			if delta > excess {
				delta = excess
			}
			widths[index] -= delta
			excess -= delta
			changed = true
		}
		if changed {
			continue
		}
		for index := len(widths) - 1; index >= 0 && excess > 0; index-- {
			if widths[index] <= 1 {
				continue
			}
			delta := widths[index] - 1
			if delta > excess {
				delta = excess
			}
			widths[index] -= delta
			excess -= delta
			changed = true
		}
		if !changed {
			return
		}
	}
}

func growTableFlexColumns(columns []TableColumn, widths []int, remaining int, available int) {
	flex := []int{}
	for index, column := range columns {
		if column.Flex && column.Width <= 0 {
			flex = append(flex, index)
		}
	}
	for remaining > 0 && len(flex) > 0 {
		changed := false
		for _, index := range flex {
			limit := columns[index].MaxWidth
			if limit <= 0 {
				limit = available
			}
			if widths[index] >= limit {
				continue
			}
			widths[index]++
			remaining--
			changed = true
			if remaining == 0 {
				break
			}
		}
		if !changed {
			return
		}
	}
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func skipANSISequence(value string, index int) int {
	index++
	if index >= len(value) {
		return index
	}
	switch value[index] {
	case '[':
		index++
		for index < len(value) {
			current := value[index]
			index++
			if current >= 0x40 && current <= 0x7e {
				break
			}
		}
	case ']':
		index++
		for index < len(value) {
			if value[index] == '\a' {
				return index + 1
			}
			if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '\\' {
				return index + 2
			}
			index++
		}
	default:
		index++
	}
	return index
}
