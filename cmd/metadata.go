package cmd

import (
	"strings"
)

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

type VolcengineMeta struct {
	ApiInfo  *ApiInfo
	Request  *MetaInfo
	Response *MetaInfo
}

type MetaInfo struct {
	Basic     *[]string
	Structure *map[string]MetaInfo
}

type ApiInfo struct {
	Method      string
	ContentType string
	ServiceName string
	ParamTypes  map[string]string
	// int float64
	// [], {}
}

type StructInfo struct {
	PkgName     string
	ServiceName string
	Version     string
}

type param struct {
	key         string
	typeName    string
	required    bool
	description string // optional help text from asset/paramdescriptions (params.json)
	example     string // optional Example from CAE (current language only)
}

// formatParamsHelpUsage formats request parameters for Action -h/--help.
// When detail is false (default help), only key/type/Required|Optional are shown.
// When detail is true (-h --detail), full description and Example bodies are included.
func formatParamsHelpUsage(params []param, detail bool) []string {
	maxKeyWidth, maxTypeWidth, maxReqWidth := paramHelpColumnWidths(params)
	// One trailing space between columns.
	maxKeyWidth++
	maxTypeWidth++
	maxReqWidth++

	if !detail {
		out := make([]string, 0, len(params))
		for _, p := range params {
			out = append(out, formatParamHelpMeta(p, maxKeyWidth, maxTypeWidth, maxReqWidth))
		}
		return out
	}

	// actionUsageTemplate prefixes each entry with "  --" (4 columns).
	// Continuation lines indent under the description column of the first line.
	descColIndent := strings.Repeat(" ", 4+maxKeyWidth+maxTypeWidth+maxReqWidth)

	var paramStrings []string
	for i, p := range params {
		meta := formatParamHelpMeta(p, maxKeyWidth, maxTypeWidth, maxReqWidth)
		// Body under the description column: multi-line description, then optional example.
		var bodyLines []string
		if desc := strings.TrimSpace(p.description); desc != "" {
			// Descriptions are baked into cobra text/templates; escape {{ }} so
			// OpenAPI text cannot break Usage() parsing. Keep multi-line body.
			desc = escapeCobraTemplateLiteral(normalizeHelpDescription(desc))
			if desc != "" {
				bodyLines = append(bodyLines, strings.Split(desc, "\n")...)
			}
		}
		if ex := strings.TrimSpace(p.example); ex != "" {
			ex = escapeCobraTemplateLiteral(normalizeHelpDescription(ex))
			if ex != "" {
				// Put example after description body so dense OpenAPI text
				// does not bury "??/Example" between description bullets.
				exLines := strings.Split(ex, "\n")
				bodyLines = append(bodyLines, tr("Example:")+" "+exLines[0])
				for _, cont := range exLines[1:] {
					bodyLines = append(bodyLines, cont)
				}
			}
		}
		// Collapse empty body lines so OpenAPI "\n\n* ..." does not create
		// large visual holes between parameters.
		bodyLines = compactHelpBodyLines(bodyLines)
		// Soft-wrap long single-line paragraphs (common in EN OpenAPI text)
		// so terminal auto-wrap does not dump continuations at column 0.
		bodyLines = wrapHelpBodyLines(bodyLines, defaultHelpDescWidth)

		line := meta
		if len(bodyLines) > 0 {
			// First description line continues the header columns.
			line = meta + bodyLines[0]
			for _, cont := range bodyLines[1:] {
				line = line + "\n" + descColIndent + cont
			}
		}

		// Separate multi-line / described parameter blocks so dense help stays scannable.
		// Pure key/type/req single-line entries stay compact without blank gaps.
		// Do NOT append an empty "" entry: actionUsageTemplate prefixes every entry
		// with "  --", which would render bare "--" lines in -h.
		if i > 0 {
			prev := paramStrings[len(paramStrings)-1]
			if strings.Contains(prev, "\n") || strings.Contains(line, "\n") || len(bodyLines) > 0 {
				paramStrings[len(paramStrings)-1] = prev + "\n"
			}
		}
		paramStrings = append(paramStrings, line)
	}

	return paramStrings
}

func paramHelpColumnWidths(params []param) (maxKeyWidth, maxTypeWidth, maxReqWidth int) {
	// Use display width (CJK-aware) so Chinese labels align in terminals.
	for _, p := range params {
		if w := displayWidth(p.key); w > maxKeyWidth {
			maxKeyWidth = w
		}
		if w := displayWidth(p.typeName); w > maxTypeWidth {
			maxTypeWidth = w
		}
		if w := displayWidth(formatRequired(p.required)); w > maxReqWidth {
			maxReqWidth = w
		}
	}
	// Also reserve width for the other polarity label so columns stay stable
	// within a single -h block under the current language.
	for _, label := range []string{formatRequired(true), formatRequired(false)} {
		if w := displayWidth(label); w > maxReqWidth {
			maxReqWidth = w
		}
	}
	return maxKeyWidth, maxTypeWidth, maxReqWidth
}

func formatParamHelpMeta(p param, maxKeyWidth, maxTypeWidth, maxReqWidth int) string {
	return padRightDisplay(p.key, maxKeyWidth) +
		padRightDisplay(p.typeName, maxTypeWidth) +
		padRightDisplay(formatRequired(p.required), maxReqWidth)
}

// displayWidth returns terminal columns for s: ASCII=1, most CJK/fullwidth=2.
// Used only for help alignment; does not alter string content.
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	switch {
	case r == 0x0000 || r == 0x000AD:
		return 0
	case r < 0x1100:
		return 1
	// Hangul Jamo / CJK ranges commonly rendered double-width in East Asian terminals.
	case r >= 0x1100 && r <= 0x115F || // Hangul Jamo
		r == 0x2329 || r == 0x232A ||
		r >= 0x2E80 && r <= 0xA4CF || // CJK ... Yi
		r >= 0xAC00 && r <= 0xD7A3 || // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF || // CJK Compatibility Ideographs
		r >= 0xFE10 && r <= 0xFE19 || // Vertical forms
		r >= 0xFE30 && r <= 0xFE6F || // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60 || // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6 ||
		r >= 0x20000 && r <= 0x2FFFD ||
		r >= 0x30000 && r <= 0x3FFFD:
		return 2
	default:
		return 1
	}
}

// padRightDisplay right-pads s with spaces until display width reaches width.
func padRightDisplay(s string, width int) string {
	pad := width - displayWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// normalizeHelpDescription trims ends and normalizes newlines for -h display.
func normalizeHelpDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSpace(s)
	// Drop trailing spaces on each line; keep internal blank lines for now.
	parts := strings.Split(s, "\n")
	for i, part := range parts {
		parts[i] = strings.TrimRight(part, " \t")
	}
	return strings.Join(parts, "\n")
}

// compactHelpBodyLines removes empty lines and trims each line so OpenAPI
// descriptions do not leave large blank gaps in -h output.
func compactHelpBodyLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// defaultHelpDescWidth is the target display width for the description column
// when wrapping long single-line OpenAPI text. Terminal auto-wrap otherwise
// restarts at column 0 and looks broken next to indented multi-line params.
const defaultHelpDescWidth = 72

// wrapHelpBodyLines soft-wraps each body line to maxWidth display columns so
// long English paragraphs stay under the description column instead of relying
// on terminal reflow. Existing bullet/list lines are preserved and only wrap
// when a single line still exceeds maxWidth.
func wrapHelpBodyLines(lines []string, maxWidth int) []string {
	if maxWidth < 20 {
		maxWidth = 20
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrapHelpDisplayLine(line, maxWidth)...)
	}
	return out
}

// wrapHelpDisplayLine wraps one logical line by display width.
// Prefer breaking on spaces for Latin text; if a token is longer than the
// remaining width (e.g. long identifiers), hard-split by rune display width.
func wrapHelpDisplayLine(line string, maxWidth int) []string {
	line = strings.TrimRight(line, " \t")
	if line == "" {
		return []string{""}
	}
	if displayWidth(line) <= maxWidth {
		return []string{line}
	}

	// Keep common list/indent prefixes on continuation lines so wrapped list
	// items stay nested under the description column.
	prefix, content := splitHelpLinePrefix(line)
	prefixWidth := displayWidth(prefix)
	contentWidth := maxWidth - prefixWidth
	if contentWidth < 16 {
		prefix = ""
		prefixWidth = 0
		content = line
		contentWidth = maxWidth
	}

	words := strings.Fields(content)
	if len(words) == 0 {
		return []string{line}
	}

	var out []string
	cur := prefix
	curWidth := prefixWidth
	atLineStart := true
	flush := func() {
		if curWidth > prefixWidth || (prefix == "" && curWidth > 0) {
			out = append(out, strings.TrimRight(cur, " "))
		} else if cur == prefix && prefix != "" && len(words) == 0 {
			// no-op
		}
		cur = prefix
		curWidth = prefixWidth
		atLineStart = true
	}

	for _, word := range words {
		parts := []string{word}
		if displayWidth(word) > contentWidth {
			parts = hardWrapByDisplayWidth(word, contentWidth)
		}
		for _, part := range parts {
			partWidth := displayWidth(part)
			sepWidth := 0
			if !atLineStart {
				sepWidth = 1
			}
			if !atLineStart && curWidth+sepWidth+partWidth > maxWidth {
				flush()
				sepWidth = 0
			}
			if !atLineStart {
				cur += " "
				curWidth++
			}
			cur += part
			curWidth += partWidth
			atLineStart = false
		}
	}
	if !atLineStart || (prefix == "" && curWidth > 0) {
		out = append(out, strings.TrimRight(cur, " "))
	}
	if len(out) == 0 {
		return []string{line}
	}
	return out
}

// splitHelpLinePrefix keeps common list/indent prefixes on wrapped continuations.
// Examples: "* ", "- ", "  * ".
func splitHelpLinePrefix(line string) (prefix, content string) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	if i < len(runes) && (runes[i] == '*' || runes[i] == '-') {
		i++
		if i < len(runes) && runes[i] == ' ' {
			i++
		}
		return string(runes[:i]), string(runes[i:])
	}
	if i > 0 {
		return string(runes[:i]), string(runes[i:])
	}
	return "", line
}

func hardWrapByDisplayWidth(s string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	if displayWidth(s) <= maxWidth {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	curW := 0
	for _, r := range s {
		rw := runeDisplayWidth(r)
		if curW > 0 && curW+rw > maxWidth {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
		// If a single rune is wider than maxWidth, still emit it alone.
		if curW == 0 && rw > maxWidth {
			out = append(out, string(r))
			continue
		}
		cur.WriteRune(r)
		curW += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// escapeCobraTemplateLiteral makes free-form text safe to embed in a cobra
// usage template that is later parsed as text/template.
func escapeCobraTemplateLiteral(s string) string {
	// {{"{{"}} and {{"}}"}} evaluate to literal braces in text/template.
	return strings.NewReplacer(
		"{{", `{{"{{"}}`,
		"}}", `{{"}}"}}`,
	).Replace(s)
}

func formatRequired(required bool) string {
	if required {
		return tr("Required")
	}
	return tr("Optional")
}

func (meta *VolcengineMeta) GetRequestParams(apiMeta *ApiMeta) (params []param) {
	var s []string
	var traverse func(MetaInfo)

	traverse = func(meta MetaInfo) {
		if meta.Basic != nil {
			for _, v := range *meta.Basic {
				s = append(s, v)
				if apiMeta == nil {
					paramKey := strings.Join(s, ".")
					params = append(params, param{
						key:      paramKey,
						typeName: "",
						required: false,
					})
				} else {
					paramKey := strings.Join(s, ".")
					params = append(params, param{
						key:      paramKey,
						typeName: apiMeta.GetReqTypeName(paramKey),
						required: apiMeta.GetReqRequired(paramKey),
					})
				}
				s = s[:len(s)-1]
			}
		}

		if meta.Structure != nil {
			for k2, v2 := range *meta.Structure {
				s = append(s, k2)
				traverse(v2)
				s = s[:len(s)-1]
			}
		}
	}

	traverse(*meta.Request)
	return
}
