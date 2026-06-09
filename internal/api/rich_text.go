package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RichTextFromParts(text string, blocks json.RawMessage, attachments json.RawMessage, files []FileResult) string {
	parts := []string{}
	seen := map[string]bool{}
	appendRenderedPart(&parts, seen, text)
	for _, part := range renderBlocks(blocks) {
		appendRenderedPart(&parts, seen, part)
	}
	for _, part := range renderAttachments(attachments) {
		appendRenderedPart(&parts, seen, part)
	}
	for _, part := range renderFiles(files) {
		appendRenderedPart(&parts, seen, part)
	}
	return strings.Join(parts, "\n")
}

func RichBlocksText(blocks json.RawMessage) string {
	return strings.Join(cleanRenderedParts(renderBlocks(blocks)), "\n")
}

func RichAttachmentsText(attachments json.RawMessage) string {
	return strings.Join(cleanRenderedParts(renderAttachments(attachments)), "\n")
}

func RichFilesText(files []FileResult) string {
	return strings.Join(cleanRenderedParts(renderFiles(files)), "\n")
}

func VisibleJSONText(raw json.RawMessage) []string {
	return cleanRenderedParts(renderGenericJSON(raw))
}

func appendRenderedPart(parts *[]string, seen map[string]bool, value string) {
	value = CleanSlackText(value)
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	*parts = append(*parts, value)
}

func cleanRenderedParts(parts []string) []string {
	cleaned := []string{}
	seen := map[string]bool{}
	for _, part := range parts {
		appendRenderedPart(&cleaned, seen, part)
	}
	return cleaned
}

func renderBlocks(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var blocks []any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		var single any
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil
		}
		return renderBlockValue(single)
	}
	parts := []string{}
	for _, block := range blocks {
		parts = append(parts, renderBlockValue(block)...)
	}
	return parts
}

func renderBlockValue(value any) []string {
	block, ok := value.(map[string]any)
	if !ok {
		return renderGenericValue(value, "")
	}
	switch stringField(block, "type") {
	case "section":
		parts := []string{}
		appendRawPart(&parts, renderTextObject(block["text"]))
		if fields, ok := block["fields"].([]any); ok {
			fieldParts := []string{}
			for _, field := range fields {
				appendRawPart(&fieldParts, renderTextObject(field))
			}
			if len(fieldParts) > 0 {
				parts = append(parts, strings.Join(fieldParts, " | "))
			}
		}
		return parts
	case "header":
		return nonEmptyParts(renderTextObject(block["text"]))
	case "context":
		return nonEmptyParts(renderInlineElements(block["elements"]))
	case "actions":
		return nonEmptyParts(renderActionElements(block["elements"]))
	case "image":
		return nonEmptyParts(firstNonEmpty(stringField(block, "title"), renderTextObject(block["title"]), stringField(block, "alt_text")))
	case "rich_text":
		return renderRichTextElements(block["elements"])
	case "table":
		return renderTableBlock(block)
	case "divider":
		return nil
	default:
		return renderGenericValue(value, "")
	}
}

func renderAttachments(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var attachments []any
	if err := json.Unmarshal(raw, &attachments); err != nil {
		var single any
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil
		}
		attachments = []any{single}
	}
	parts := []string{}
	for _, value := range attachments {
		attachment, ok := value.(map[string]any)
		if !ok {
			parts = append(parts, renderGenericValue(value, "")...)
			continue
		}
		for _, key := range []string{"pretext", "author_name", "title", "text", "fallback", "footer"} {
			appendRawPart(&parts, stringField(attachment, key))
		}
		if fields, ok := attachment["fields"].([]any); ok {
			for _, fieldValue := range fields {
				field, ok := fieldValue.(map[string]any)
				if !ok {
					continue
				}
				title := stringField(field, "title")
				value := stringField(field, "value")
				if title != "" && value != "" {
					parts = append(parts, title+": "+value)
				} else {
					appendRawPart(&parts, firstNonEmpty(title, value))
				}
			}
		}
		if actions, ok := attachment["actions"]; ok {
			appendRawPart(&parts, renderActionElements(actions))
		}
	}
	return parts
}

func renderFiles(files []FileResult) []string {
	parts := []string{}
	for _, file := range files {
		label := firstNonEmpty(file.Title, file.Name, file.ID)
		if label == "" {
			continue
		}
		meta := []string{}
		if file.Filetype != "" {
			meta = append(meta, file.Filetype)
		} else if file.Mimetype != "" {
			meta = append(meta, file.Mimetype)
		}
		if file.Size > 0 {
			meta = append(meta, fmt.Sprintf("%d bytes", file.Size))
		}
		line := "File: " + label
		if len(meta) > 0 {
			line += " (" + strings.Join(meta, ", ") + ")"
		}
		if file.Permalink != "" {
			line += " " + file.Permalink
		} else if file.URLPrivate != "" {
			line += " " + file.URLPrivate
		}
		parts = append(parts, line)
	}
	return parts
}

func renderRichTextElements(value any) []string {
	elements, ok := value.([]any)
	if !ok {
		return nil
	}
	parts := []string{}
	for _, elementValue := range elements {
		element, ok := elementValue.(map[string]any)
		if !ok {
			continue
		}
		switch stringField(element, "type") {
		case "rich_text_section":
			appendRawPart(&parts, renderInlineElements(element["elements"]))
		case "rich_text_list":
			parts = append(parts, renderRichTextList(element)...)
		case "rich_text_preformatted":
			appendRawPart(&parts, "``` "+renderInlineElements(element["elements"])+" ```")
		case "rich_text_quote":
			appendRawPart(&parts, "Quote: "+renderInlineElements(element["elements"]))
		default:
			appendRawPart(&parts, renderRichTextNode(element))
		}
	}
	return parts
}

func renderRichTextList(element map[string]any) []string {
	style := stringField(element, "style")
	if style == "" {
		style = "bullet"
	}
	children, _ := element["elements"].([]any)
	lines := []string{}
	for index, child := range children {
		text := renderRichTextNode(child)
		if text == "" {
			continue
		}
		prefix := "- "
		if style == "ordered" {
			prefix = fmt.Sprintf("%d. ", index+1)
		}
		lines = append(lines, prefix+text)
	}
	return lines
}

func renderInlineElements(value any) string {
	elements, ok := value.([]any)
	if !ok {
		return renderRichTextNode(value)
	}
	parts := []string{}
	for _, element := range elements {
		appendRawPart(&parts, renderRichTextNode(element))
	}
	return strings.Join(parts, " ")
}

func renderActionElements(value any) string {
	elements, ok := value.([]any)
	if !ok {
		return renderTextObject(value)
	}
	parts := []string{}
	for _, element := range elements {
		appendRawPart(&parts, renderActionElement(element))
	}
	return strings.Join(parts, " | ")
}

func renderActionElement(value any) string {
	element, ok := value.(map[string]any)
	if !ok {
		return renderTextObject(value)
	}
	text := renderTextObject(element["text"])
	if text == "" {
		text = renderTextObject(element["placeholder"])
	}
	return firstNonEmpty(text, stringField(element, "label"), stringField(element, "name"))
}

func renderRichTextNode(value any) string {
	node, ok := value.(map[string]any)
	if !ok {
		if text, ok := value.(string); ok {
			return text
		}
		return ""
	}
	switch stringField(node, "type") {
	case "text":
		return stringField(node, "text")
	case "link":
		url := stringField(node, "url")
		text := firstNonEmpty(stringField(node, "text"), url)
		if url == "" || text == url {
			return text
		}
		return text + " (" + url + ")"
	case "user":
		id := stringField(node, "user_id", "user")
		if id == "" {
			return ""
		}
		return "<@" + id + ">"
	case "usergroup":
		id := stringField(node, "usergroup_id", "usergroup")
		if id == "" {
			return ""
		}
		return "<!subteam^" + id + ">"
	case "channel":
		id := stringField(node, "channel_id", "channel")
		if id == "" {
			return ""
		}
		return "<#" + id + ">"
	case "emoji":
		name := stringField(node, "name")
		if name == "" {
			return ""
		}
		return ":" + name + ":"
	case "broadcast":
		rangeName := stringField(node, "range")
		if rangeName == "" {
			return ""
		}
		return "<!" + rangeName + ">"
	case "date":
		return stringField(node, "fallback")
	case "rich_text_section", "rich_text_preformatted", "rich_text_quote":
		return strings.Join(renderRichTextElements([]any{node}), " ")
	default:
		if text := renderTextObject(node["text"]); text != "" {
			return text
		}
		if elements, ok := node["elements"]; ok {
			return renderInlineElements(elements)
		}
		return ""
	}
}

func renderTableBlock(block map[string]any) []string {
	rows, ok := block["rows"].([]any)
	if !ok {
		return nil
	}
	lines := []string{}
	for _, rowValue := range rows {
		cells := tableRowCells(rowValue)
		if len(cells) == 0 {
			continue
		}
		lines = append(lines, strings.Join(cells, " | "))
	}
	return lines
}

func tableRowCells(rowValue any) []string {
	var rawCells []any
	switch row := rowValue.(type) {
	case []any:
		rawCells = row
	case map[string]any:
		rawCells, _ = row["cells"].([]any)
	}
	cells := []string{}
	for _, cellValue := range rawCells {
		text := renderTableCell(cellValue)
		if text != "" {
			cells = append(cells, text)
		}
	}
	return cells
}

func renderTableCell(value any) string {
	cell, ok := value.(map[string]any)
	if !ok {
		return renderTextObject(value)
	}
	if text := renderTextObject(cell["text"]); text != "" {
		return text
	}
	if elements, ok := cell["elements"]; ok {
		return renderInlineElements(elements)
	}
	return strings.Join(renderGenericValue(cell, ""), " ")
}

func renderTextObject(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if text := stringField(typed, "text"); text != "" {
			return text
		}
		if text := stringField(typed, "plain_text"); text != "" {
			return text
		}
		if elements, ok := typed["elements"]; ok {
			return renderInlineElements(elements)
		}
	case string:
		return typed
	}
	return ""
}

func renderGenericJSON(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return renderGenericValue(value, "")
}

func renderGenericValue(value any, key string) []string {
	parts := []string{}
	switch typed := value.(type) {
	case map[string]any:
		if fieldValue, ok := typed["value"].(string); ok {
			if _, hasTitle := typed["title"]; hasTitle {
				parts = append(parts, fieldValue)
			}
		}
		for childKey, childValue := range typed {
			parts = append(parts, renderGenericValue(childValue, childKey)...)
		}
	case []any:
		for _, childValue := range typed {
			parts = append(parts, renderGenericValue(childValue, key)...)
		}
	case string:
		if visibleJSONKey(key) {
			parts = append(parts, typed)
		}
	}
	return parts
}

func visibleJSONKey(key string) bool {
	switch strings.ToLower(key) {
	case "text", "fallback", "title", "alt_text", "label", "name", "plain_text", "preview", "description":
		return true
	default:
		return false
	}
}

func stringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmptyParts(values ...string) []string {
	parts := []string{}
	for _, value := range values {
		appendRawPart(&parts, value)
	}
	return parts
}

func appendRawPart(parts *[]string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	*parts = append(*parts, value)
}
