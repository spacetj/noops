package ops

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Filter DSL: Property[:type].operator[=value]
// Examples:
//  Title:title.equals="My Task"
//  Status:status.equals=In Progress
//  Priority:select.equals=High
//  Due:date.on_or_before=2025-10-31
//  Do:date.on_or_after=2025-10-01
//  Title:title.is_empty
//  Project:relation.contains=<page-id>

var wsRe = regexp.MustCompile(`^\s+|\s+$`)

type Query struct {
	Filter      any    `json:"filter,omitempty"`
	Sorts       []any  `json:"sorts,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
	StartCursor string `json:"start_cursor,omitempty"`
}

// BuildFilter builds a Notion filter object from a list of DSL specs combined with logical AND.
func BuildFilter(specs []string) (map[string]any, error) {
	var andParts []any
	for _, s := range specs {
		s = wsRe.ReplaceAllString(s, "")
		if s == "" {
			continue
		}
		// split property[:type].operator[=value]
		// find '.'
		dot := strings.IndexByte(s, '.')
		if dot <= 0 {
			return nil, fmt.Errorf("invalid filter spec (missing '.') in %q", s)
		}
		left, rest := s[:dot], s[dot+1:]
		name, typ := left, ""
		if i := strings.IndexByte(left, ':'); i >= 0 {
			name = left[:i]
			typ = left[i+1:]
		}
		op := rest
		val := ""
		if i := strings.IndexByte(rest, '='); i >= 0 {
			op = rest[:i]
			val = rest[i+1:]
			val = strings.Trim(val, "\"'")
		}
		// relation contains expects an id normalization, but leave raw here; caller may normalize
		part := map[string]any{
			"property": name,
		}
		var cond map[string]any
		if typ == "" {
			typ = inferTypeFromOp(op)
		}
		switch typ {
		case "title", "rich_text":
			cond = map[string]any{typ: map[string]any{op: valOrBool(op, val)}}
		case "status", "select":
			cond = map[string]any{typ: map[string]any{op: valOrBool(op, val)}}
		case "date":
			cond = map[string]any{typ: map[string]any{op: dateVal(op, val)}}
		case "checkbox":
			vb := strings.EqualFold(val, "true") || val == "1"
			cond = map[string]any{typ: map[string]any{op: vb}}
		case "relation":
			// Relation filters:
			// - contains / does_not_contain expect a page UUID
			// - is_empty / is_not_empty are booleans
			switch op {
			case "contains", "does_not_contain":
				cond = map[string]any{typ: map[string]any{op: normalizePageID(val)}}
			default:
				cond = map[string]any{typ: map[string]any{op: true}}
			}
		case "number":
			cond = map[string]any{typ: map[string]any{op: mustNumber(val)}}
		default:
			// Fallback assume rich_text semantics
			cond = map[string]any{"rich_text": map[string]any{op: valOrBool(op, val)}}
		}
		for k, v := range cond {
			part[k] = v
		}
		andParts = append(andParts, part)
	}
	if len(andParts) == 0 {
		return nil, nil
	}
	return map[string]any{"and": andParts}, nil
}

func inferTypeFromOp(op string) string {
	switch op {
	case "equals", "does_not_equal", "contains", "does_not_contain", "starts_with", "ends_with", "is_empty", "is_not_empty":
		return "rich_text"
	case "on_or_after", "on_or_before", "past_week", "past_month", "past_year", "next_week", "next_month", "next_year", "after", "before", "on" /*alias*/ :
		return "date"
	default:
		return "rich_text"
	}
}

func valOrBool(op, val string) any {
	switch op {
	case "is_empty", "is_not_empty":
		return true
	default:
		return val
	}
}

func dateVal(op, val string) any {
	switch op {
	case "is_empty", "is_not_empty", "past_week", "past_month", "past_year", "next_week", "next_month", "next_year":
		return true
	case "on": // allow alias -> equals semantics for date (Notion uses equals)
		return map[string]any{"equals": val}
	default:
		return val
	}
}

func mustNumber(s string) float64 {
	// simple parse; ignore errors -> 0
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}

// --- Minimal ID normalization for relation filter values ---
func normalizePageID(input string) string {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return s
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	// uuid in path
	for i := 0; i+36 <= len(s); i++ {
		sub := s[i : i+36]
		if fIsUUID(sub) {
			return sub
		}
	}
	for i := 0; i+32 <= len(s); i++ {
		sub := s[i : i+32]
		if fIsHex(sub) {
			return fHyphenate32(sub)
		}
	}
	if len(s) == 32 && fIsHex(s) {
		return fHyphenate32(s)
	}
	if len(s) == 36 && fIsUUID(s) {
		return s
	}
	return s
}
func fIsHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func fIsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
			continue
		}
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func fHyphenate32(s string) string {
	if len(s) != 32 || !fIsHex(s) {
		return s
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// BuildSorts parses "Prop[:type]:asc|desc" specs
func BuildSorts(specs []string) ([]any, error) {
	var out []any
	for _, s := range specs {
		s = wsRe.ReplaceAllString(s, "")
		if s == "" {
			continue
		}
		dir := "ascending"
		if i := strings.LastIndexByte(s, ':'); i >= 0 {
			d := strings.ToLower(s[i+1:])
			if d == "desc" || d == "descending" {
				dir = "descending"
			}
			s = s[:i]
		}
		name := s
		if i := strings.IndexByte(s, ':'); i >= 0 {
			name = s[:i]
		}
		out = append(out, map[string]any{
			"property":  name,
			"direction": dir,
		})
	}
	return out, nil
}

// MarshalQuery utility
func MarshalQuery(filter map[string]any, sorts []any, pageSize int, cursor string) ([]byte, error) {
	q := Query{Sorts: sorts}
	if filter != nil {
		q.Filter = filter
	}
	if pageSize > 0 {
		q.PageSize = pageSize
	}
	if cursor != "" {
		q.StartCursor = cursor
	}
	return json.Marshal(q)
}

// ===== Read helpers for common property types (used by friendly commands) =====

func ReadStatus(v any) string {
	m, _ := v.(map[string]any)
	st, _ := m["status"].(map[string]any)
	name, _ := st["name"].(string)
	return name
}

func ReadDateStart(v any) string {
	m, _ := v.(map[string]any)
	d, _ := m["date"].(map[string]any)
	s, _ := d["start"].(string)
	return s
}

func ReadFirstRelationID(v any) string {
	m, _ := v.(map[string]any)
	arr, _ := m["relation"].([]any)
	if len(arr) == 0 {
		return ""
	}
	f, _ := arr[0].(map[string]any)
	id, _ := f["id"].(string)
	return id
}
