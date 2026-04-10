package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spacetj/noops/internal/notion"
)

type Runner struct {
	c             *notion.Client
	log           *log.Logger
	dbID          string
	goalID        string
	dbJSON        map[string]any
	title         string
	titleID       string
	statusK       string
	relSelf       string
	goalRel       string
	goalDB        string
	goalTitleKey  string
	goalStatusKey string
	goalMetaReady bool
}

func NewRunner(c *notion.Client, logger *log.Logger) *Runner {
	return &Runner{c: c, log: logger}
}

func (r *Runner) SetDBID(id string)             { r.dbID = id }
func (r *Runner) SetGoalPageID(id string)       { r.goalID = id }
func (r *Runner) Client() *notion.Client        { return r.c }
func (r *Runner) TitleDB() string               { return r.dbID }
func (r *Runner) StatusKey() string             { return r.statusK }
func (r *Runner) SelfRelationKey() string       { return r.relSelf }
func (r *Runner) GoalsRelationKey() string      { return r.goalRel }
func (r *Runner) GoalsDatabaseID() string       { return r.goalDB }
func (r *Runner) HasProp(name, typ string) bool { return r.hasProp(name, typ) }

func (r *Runner) fetchDB(ctx context.Context) error {
	b, code, err := r.c.GetDatabase(r.dbID)
	if err != nil {
		return err
	}
	if code/100 != 2 {
		return fmt.Errorf("get database %s: HTTP %d: %s", r.dbID, code, string(b))
	}
	if err := json.Unmarshal(b, &r.dbJSON); err != nil {
		return err
	}
	r.title, r.titleID = detectTitleAndID(r.dbJSON)
	r.statusK = detectFirstPropOfType(r.dbJSON, "status")
	r.relSelf = detectSelfRelation(r.dbJSON, r.dbID)
	r.goalRel, r.goalDB = detectExternalRelation(r.dbJSON, r.dbID)
	r.goalMetaReady = false
	r.log.Printf("DB title property: %s, status: %s, self-relation: %s, goal relation: %s -> %s", r.title, r.statusK, r.relSelf, r.goalRel, r.goalDB)
	return nil
}

// Exported wrappers for commands
func (r *Runner) FetchDB(ctx context.Context) error { return r.fetchDB(ctx) }
func (r *Runner) TitleKey() string                  { return r.title }
func TitleProp(title string) map[string]any {
	return map[string]any{"title": []any{map[string]any{"text": map[string]any{"content": title}}}}
}

// BuildPropsFromSpecs parses property set specs into a properties map.
// Spec formats:
//   - name=value (attempt to infer type)
//   - name:date=YYYY-MM-DD
//   - name:select=Option
//   - name:status=StatusName
//   - name:relation=<page-id-or-url>
//   - name:clear
func (r *Runner) BuildPropsFromSpecs(specs []string) (map[string]any, []string, error) {
	props := map[string]any{}
	var warns []string
	for _, s := range specs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasSuffix(s, ":clear") {
			name := strings.TrimSuffix(s, ":clear")
			if r.hasProp(name, "date") {
				props[name] = map[string]any{"date": nil}
			} else if r.hasProp(name, "status") {
				props[name] = map[string]any{"status": nil}
			} else if r.hasProp(name, "select") {
				props[name] = map[string]any{"select": nil}
			} else if r.hasProp(name, "relation") {
				props[name] = map[string]any{"relation": []any{}}
			} else if name == r.title {
				props[name] = TitleProp("")
			} else {
				warns = append(warns, "unknown property: "+name)
			}
			continue
		}
		var name, kind, val string
		if i := strings.IndexByte(s, '='); i >= 0 {
			left, right := s[:i], s[i+1:]
			if j := strings.IndexByte(left, ':'); j >= 0 {
				name = left[:j]
				kind = left[j+1:]
			} else {
				name = left
				kind = ""
			}
			val = right
		} else {
			return nil, warns, fmt.Errorf("invalid --set spec: %q", s)
		}
		val = strings.Trim(val, "\"'")
		switch kind {
		case "date":
			if !r.hasProp(name, "date") {
				warns = append(warns, name+":date not in schema")
				continue
			}
			props[name] = map[string]any{"date": map[string]any{"start": val}}
		case "select":
			if !r.hasProp(name, "select") {
				warns = append(warns, name+":select not in schema")
				continue
			}
			props[name] = map[string]any{"select": map[string]any{"name": val}}
		case "status":
			k := name
			if name == "" && r.statusK != "" {
				k = r.statusK
			}
			if !r.hasProp(k, "status") {
				warns = append(warns, k+":status not in schema")
				continue
			}
			props[k] = map[string]any{"status": map[string]any{"name": val}}
		case "relation":
			if !r.hasProp(name, "relation") {
				warns = append(warns, name+":relation not in schema")
				continue
			}
			rid := normalizeID(val)
			if rid == "" {
				return nil, warns, fmt.Errorf("invalid relation id: %q", val)
			}
			props[name] = map[string]any{"relation": []any{map[string]any{"id": rid}}}
		case "":
			if name == r.title {
				props[name] = TitleProp(val)
			} else if r.hasProp(name, "select") {
				props[name] = map[string]any{"select": map[string]any{"name": val}}
			} else if r.hasProp(name, "status") {
				props[name] = map[string]any{"status": map[string]any{"name": val}}
			} else if r.hasProp(name, "date") {
				props[name] = map[string]any{"date": map[string]any{"start": val}}
			} else {
				warns = append(warns, "unknown property: "+name)
			}
		default:
			return nil, warns, fmt.Errorf("unknown kind %q in spec %q", kind, s)
		}
	}
	return props, warns, nil
}

// Utility normalize for IDs/URLs (simplified copy from cmd)
func normalizeID(input string) string {
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
		if isUUID(sub) {
			return sub
		}
	}
	for i := 0; i+32 <= len(s); i++ {
		sub := s[i : i+32]
		if isHex(sub) {
			return hyphenate32(sub)
		}
	}
	if len(s) == 32 && isHex(s) {
		return hyphenate32(s)
	}
	if len(s) == 36 && isUUID(s) {
		return s
	}
	return s
}
func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return len(s) > 0
}
func isUUID(s string) bool {
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
func hyphenate32(s string) string {
	if len(s) != 32 || !isHex(s) {
		return s
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

// ApplyProps performs PATCH with given properties
func (r *Runner) ApplyProps(ctx context.Context, id string, props map[string]any) error {
	if len(props) == 0 {
		return nil
	}
	payload := map[string]any{"properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.UpdatePage(id, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("update page: %d %v %s", code, err, string(b))
	}
	return nil
}

// ApplyEmoji sets the page icon to an emoji.
func (r *Runner) ApplyEmoji(ctx context.Context, id, emoji string) error {
	if emoji == "" {
		return nil
	}
	payload := map[string]any{"icon": map[string]any{"type": "emoji", "emoji": emoji}}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.UpdatePage(id, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("update emoji: %d %v %s", code, err, string(b))
	}
	return nil
}

// AppendPageNotes appends paragraph blocks to the given page id from the provided text.
// The text is split into paragraphs by double newlines; each paragraph is chunked into
// <=1800 char rich_text pieces to satisfy Notion limits.
func (r *Runner) AppendPageNotes(ctx context.Context, pageID, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	paras := splitParagraphs(text)
	children := make([]any, 0, len(paras))
	for _, p := range paras {
		if p == "" {
			p = " "
		}
		chunks := chunkText(p, 1800)
		rts := make([]any, 0, len(chunks))
		for _, c := range chunks {
			rts = append(rts, map[string]any{"type": "text", "text": map[string]any{"content": c}})
		}
		children = append(children, map[string]any{
			"object":    "block",
			"type":      "paragraph",
			"paragraph": map[string]any{"rich_text": rts},
		})
	}
	payload := map[string]any{"children": children}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.AppendBlockChildren(pageID, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("append notes: %d %v %s", code, err, string(b))
	}
	return nil
}

func splitParagraphs(s string) []string {
	// Split on blank lines; normalize CRLF
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p)
	}
	return out
}

func chunkText(s string, max int) []string {
	if max <= 0 {
		return []string{s}
	}
	res := []string{}
	for i := 0; i < len(s); i += max {
		j := i + max
		if j > len(s) {
			j = len(s)
		}
		res = append(res, s[i:j])
	}
	return res
}

func (r *Runner) CreateWithProps(ctx context.Context, title string, props map[string]any) (string, error) {
	if props == nil {
		props = map[string]any{}
	}
	props[r.title] = TitleProp(title)
	payload := map[string]any{"parent": map[string]any{"database_id": r.dbID}, "properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return id, nil
}

// CreateWithPropsEmoji creates a page with properties and an optional emoji icon.
func (r *Runner) CreateWithPropsEmoji(ctx context.Context, title string, props map[string]any, emoji string) (string, error) {
	if props == nil {
		props = map[string]any{}
	}
	props[r.title] = TitleProp(title)
	payload := map[string]any{
		"parent":     map[string]any{"database_id": r.dbID},
		"properties": props,
	}
	if emoji != "" {
		payload["icon"] = map[string]any{"type": "emoji", "emoji": emoji}
	}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return id, nil
}

func (r *Runner) FirstByTitle(ctx context.Context, title string) string {
	return r.firstByTitle(ctx, title)
}
func (r *Runner) ArchivePage(ctx context.Context, id string) error { return r.archivePage(ctx, id) }

func (r *Runner) ensureGoalMeta(ctx context.Context) error {
	if r.goalDB == "" {
		return fmt.Errorf("tasks database missing Goals relation (set --goal or run noops init)")
	}
	if r.goalMetaReady {
		return nil
	}
	b, code, err := r.c.GetDatabase(r.goalDB)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get goals database: %d %v %s", code, err, string(b))
	}
	var meta map[string]any
	if err := json.Unmarshal(b, &meta); err != nil {
		return err
	}
	r.goalTitleKey, _ = detectTitleAndID(meta)
	if r.goalTitleKey == "" {
		r.goalTitleKey = "Name"
	}
	r.goalStatusKey = detectFirstPropOfType(meta, "status")
	r.goalMetaReady = true
	return nil
}

func (r *Runner) CreateGoal(ctx context.Context, title, status, emoji string) (string, error) {
	if err := r.ensureGoalMeta(ctx); err != nil {
		return "", err
	}
	props := map[string]any{
		r.goalTitleKey: TitleProp(title),
	}
	if status != "" && r.goalStatusKey != "" {
		props[r.goalStatusKey] = map[string]any{"status": map[string]any{"name": status}}
	}
	payload := map[string]any{
		"parent":     map[string]any{"database_id": r.goalDB},
		"properties": props,
	}
	if emoji != "" {
		payload["icon"] = map[string]any{"type": "emoji", "emoji": emoji}
	}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create goal: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return id, nil
}

func (r *Runner) UpdateGoal(ctx context.Context, id, title, status, emoji string) error {
	if err := r.ensureGoalMeta(ctx); err != nil {
		return err
	}
	props := map[string]any{}
	if title != "" {
		props[r.goalTitleKey] = TitleProp(title)
	}
	if status != "" && r.goalStatusKey != "" {
		props[r.goalStatusKey] = map[string]any{"status": map[string]any{"name": status}}
	}
	if err := r.ApplyProps(ctx, id, props); err != nil {
		return err
	}
	if emoji != "" {
		if err := r.ApplyEmoji(ctx, id, emoji); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) ArchiveGoal(ctx context.Context, id string) error {
	return r.archivePage(ctx, id)
}

func (r *Runner) GoalFirstByTitle(ctx context.Context, title string) string {
	if err := r.ensureGoalMeta(ctx); err != nil {
		return ""
	}
	filter := map[string]any{
		"filter": map[string]any{
			"property": r.goalTitleKey,
			"title":    map[string]any{"equals": title},
		},
		"page_size": 1,
	}
	body, _ := json.Marshal(filter)
	b, code, err := r.c.QueryDatabase(r.goalDB, body)
	if err != nil || code/100 != 2 {
		r.log.Printf("query goal title %q: %d %v %s", title, code, err, string(b))
		return ""
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	if len(results) == 0 {
		return ""
	}
	row, _ := results[0].(map[string]any)
	id, _ := row["id"].(string)
	return id
}

func detectTitleAndID(db map[string]any) (string, string) {
	props, _ := db["properties"].(map[string]any)
	for k, v := range props {
		if m, ok := v.(map[string]any); ok && m["type"] == "title" {
			id, _ := m["id"].(string)
			return k, id
		}
	}
	return "Name", "title"
}

func detectFirstPropOfType(db map[string]any, t string) string {
	props, _ := db["properties"].(map[string]any)
	for k, v := range props {
		if m, ok := v.(map[string]any); ok && m["type"] == t {
			return k
		}
	}
	return ""
}

func detectSelfRelation(db map[string]any, dbID string) string {
	props, _ := db["properties"].(map[string]any)
	for k, v := range props {
		if m, ok := v.(map[string]any); ok && m["type"] == "relation" {
			rel, _ := m["relation"].(map[string]any)
			if rel != nil {
				if rel["database_id"] == dbID {
					return k
				}
			}
		}
	}
	return ""
}

func detectExternalRelation(db map[string]any, selfID string) (string, string) {
	props, _ := db["properties"].(map[string]any)
	for k, v := range props {
		m, ok := v.(map[string]any)
		if !ok || m["type"] != "relation" {
			continue
		}
		rel, _ := m["relation"].(map[string]any)
		if rel == nil {
			continue
		}
		if rel["database_id"] == selfID {
			continue
		}
		if dbID, _ := rel["database_id"].(string); dbID != "" {
			return k, dbID
		}
	}
	return "", ""
}

func (r *Runner) Run(ctx context.Context) error {
	if r.dbID == "" {
		return errors.New("db id required")
	}
	if err := r.fetchDB(ctx); err != nil {
		return err
	}
	r.log.Printf("Cleaning untitled pages…")
	if err := r.CleanupUntitled(ctx); err != nil {
		return err
	}
	r.log.Printf("Running smoke checks…")
	if err := r.Tests(ctx); err != nil {
		return err
	}
	return nil
}

func (r *Runner) Tests(ctx context.Context) error {
	if r.dbID == "" {
		return errors.New("db id required")
	}
	if err := r.fetchDB(ctx); err != nil {
		return err
	}
	if r.title == "" {
		return fmt.Errorf("unable to detect title property in tasks database")
	}
	// Lightweight query to confirm access and schema readability.
	req := map[string]any{"page_size": 1}
	body, _ := json.Marshal(req)
	b, code, err := r.c.QueryDatabase(r.dbID, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("query database: %d %v %s", code, err, string(b))
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("decode query response: %w", err)
	}
	results, _ := payload["results"].([]any)
	if len(results) > 0 {
		if row, ok := results[0].(map[string]any); ok {
			if props, ok := row["properties"].(map[string]any); ok {
				_ = ReadTitle(props[r.title])
			}
		}
	}
	if r.goalID != "" {
		if b, code, err := r.c.GetPage(r.goalID); err != nil || code/100 != 2 {
			return fmt.Errorf("goal page not accessible: %d %v %s", code, err, string(b))
		}
	}
	r.log.Printf("Database %s reachable; properties OK", r.dbID)
	return nil
}

// TestAll runs an extensive E2E test suite:
// - create a test task with full properties (title, dates, priority, status, goal relation)
// - verify fields
// - update fields and verify
// - archive (delete) and verify
// - if self-relation exists: create parent/child and verify relation
func (r *Runner) TestAll(ctx context.Context) error {
	if r.dbID == "" || r.goalID == "" {
		return fmt.Errorf("test-all requires --db and --goal")
	}
	if err := r.fetchDB(ctx); err != nil {
		return err
	}
	// sanity goal page
	if b, code, _ := r.c.GetPage(r.goalID); code/100 != 2 {
		return fmt.Errorf("goal page not accessible: %d %s", code, string(b))
	}
	ts := timeNow()
	base := fmt.Sprintf("[TEST] noops %s", ts)
	title := base
	// choose options
	prio := r.chooseSelectOption("Priority", "Medium")
	status := r.chooseStatusOption("Not Started")
	do := todayPlus(0)
	due := todayPlus(7)
	// create
	id, err := r.createWithProps(ctx, title, do, due, prio, status, true)
	if err != nil {
		return err
	}
	// verify create
	if err := r.verifyProps(ctx, id, title, do, due, prio, status, true); err != nil {
		return fmt.Errorf("verify create: %w", err)
	}
	// update
	newTitle := title + " (updated)"
	newDo := todayPlus(1)
	newDue := todayPlus(10)
	newPrio := r.chooseDifferentSelect("Priority", prio)
	newStatus := r.chooseDifferentStatus(status)
	if err := r.updateFields(ctx, id, newTitle, newDo, newDue, newPrio, newStatus); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := r.verifyProps(ctx, id, newTitle, newDo, newDue, newPrio, newStatus, true); err != nil {
		return fmt.Errorf("verify update: %w", err)
	}
	// parent/child relation test if available
	if r.relSelf != "" {
		ptitle := base + " Parent"
		ctitle := base + " Child"
		pid, _ := r.ensureTask(ctx, ptitle)
		cid, _ := r.ensureTask(ctx, ctitle)
		_ = r.safeSetGoal(ctx, pid)
		_ = r.safeSetGoal(ctx, cid)
		if err := r.setParent(ctx, cid, pid); err != nil {
			return fmt.Errorf("set parent: %w", err)
		}
		// verify relation exists on child
		if err := r.verifyRelation(ctx, cid, r.relSelf, pid); err != nil {
			return fmt.Errorf("verify parent relation: %w", err)
		}
		// cleanup parent/child
		_ = r.archivePage(ctx, cid)
		_ = r.archivePage(ctx, pid)
	}
	// delete
	if err := r.archivePage(ctx, id); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	if err := r.verifyArchived(ctx, id); err != nil {
		return fmt.Errorf("verify archive: %w", err)
	}
	r.log.Printf("E2E test-all completed successfully")
	return nil
}

func (r *Runner) archivePage(ctx context.Context, id string) error {
	b, code, err := r.c.UpdatePage(id, []byte(`{"archived":true}`))
	if err != nil || code/100 != 2 {
		return fmt.Errorf("archive page: %d %v %s", code, err, string(b))
	}
	return nil
}

func (r *Runner) verifyArchived(ctx context.Context, id string) error {
	b, code, err := r.c.GetPage(id)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if arch, _ := m["archived"].(bool); !arch {
		return fmt.Errorf("page not archived")
	}
	return nil
}

func (r *Runner) verifyRelation(ctx context.Context, pageID, prop, expectedID string) error {
	b, code, err := r.c.GetPage(pageID)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	props, _ := m["properties"].(map[string]any)
	rel, _ := props[prop].(map[string]any)
	arr, _ := rel["relation"].([]any)
	for _, it := range arr {
		ri, _ := it.(map[string]any)
		if ri["id"] == expectedID {
			return nil
		}
	}
	return fmt.Errorf("relation %s does not include %s", prop, expectedID)
}

func (r *Runner) createWithProps(ctx context.Context, title, do, due, prio, status string, withGoal bool) (string, error) {
	props := map[string]any{
		r.title: map[string]any{"title": []any{map[string]any{"text": map[string]any{"content": title}}}},
	}
	if withGoal && r.goalID != "" && r.hasProp("Goals", "relation") {
		props["Goals"] = map[string]any{"relation": []any{map[string]any{"id": r.goalID}}}
	}
	if r.hasProp("Do", "date") {
		props["Do"] = map[string]any{"date": map[string]any{"start": do}}
	}
	if r.hasProp("Due", "date") {
		props["Due"] = map[string]any{"date": map[string]any{"start": due}}
	}
	if r.hasProp("Priority", "select") && prio != "" {
		props["Priority"] = map[string]any{"select": map[string]any{"name": prio}}
	}
	if r.statusK != "" && status != "" {
		props[r.statusK] = map[string]any{"status": map[string]any{"name": status}}
	}
	payload := map[string]any{"parent": map[string]any{"database_id": r.dbID}, "properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return id, nil
}

func (r *Runner) updateFields(ctx context.Context, id, title, do, due, prio, status string) error {
	props := map[string]any{}
	if title != "" {
		props[r.title] = map[string]any{"title": []any{map[string]any{"text": map[string]any{"content": title}}}}
	}
	if r.hasProp("Do", "date") && do != "" {
		props["Do"] = map[string]any{"date": map[string]any{"start": do}}
	}
	if r.hasProp("Due", "date") && due != "" {
		props["Due"] = map[string]any{"date": map[string]any{"start": due}}
	}
	if r.hasProp("Priority", "select") && prio != "" {
		props["Priority"] = map[string]any{"select": map[string]any{"name": prio}}
	}
	if r.statusK != "" && status != "" {
		props[r.statusK] = map[string]any{"status": map[string]any{"name": status}}
	}
	if len(props) == 0 {
		return nil
	}
	payload := map[string]any{"properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.UpdatePage(id, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("update page: %d %v %s", code, err, string(b))
	}
	return nil
}

func (r *Runner) verifyProps(ctx context.Context, id, title, do, due, prio, status string, goal bool) error {
	b, code, err := r.c.GetPage(id)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	props, _ := m["properties"].(map[string]any)
	// title
	if title != "" {
		t := ReadTitle(props[r.title])
		if t != title {
			return fmt.Errorf("title=%q want %q", t, title)
		}
	}
	// dates
	if do != "" && r.hasProp("Do", "date") {
		if !dateMatches(props["Do"], do) {
			return fmt.Errorf("Do mismatch")
		}
	}
	if due != "" && r.hasProp("Due", "date") {
		if !dateMatches(props["Due"], due) {
			return fmt.Errorf("Due mismatch")
		}
	}
	// select/status
	if prio != "" && r.hasProp("Priority", "select") {
		if !selectMatches(props["Priority"], prio) {
			return fmt.Errorf("Priority mismatch")
		}
	}
	if status != "" && r.statusK != "" {
		if !statusMatches(props[r.statusK], status) {
			return fmt.Errorf("Status mismatch")
		}
	}
	// goal relation
	if goal && r.goalID != "" && r.hasProp("Goals", "relation") {
		rel := props["Goals"].(map[string]any)
		arr, _ := rel["relation"].([]any)
		ok := false
		for _, it := range arr {
			rid := it.(map[string]any)["id"].(string)
			if rid == r.goalID {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("goal relation missing")
		}
	}
	return nil
}

func ReadTitle(v any) string {
	m, _ := v.(map[string]any)
	arr, _ := m["title"].([]any)
	if len(arr) == 0 {
		return ""
	}
	f, _ := arr[0].(map[string]any)
	text, _ := f["text"].(map[string]any)
	t, _ := text["content"].(string)
	return t
}

func dateMatches(v any, want string) bool {
	m, _ := v.(map[string]any)
	d, _ := m["date"].(map[string]any)
	s, _ := d["start"].(string)
	return s == want
}

func selectMatches(v any, want string) bool {
	m, _ := v.(map[string]any)
	s, _ := m["select"].(map[string]any)
	n, _ := s["name"].(string)
	return n == want
}

func statusMatches(v any, want string) bool {
	m, _ := v.(map[string]any)
	s, _ := m["status"].(map[string]any)
	n, _ := s["name"].(string)
	return n == want
}

func (r *Runner) chooseSelectOption(prop, pref string) string {
	if !r.hasProp(prop, "select") {
		return ""
	}
	props, _ := r.dbJSON["properties"].(map[string]any)
	p := props[prop].(map[string]any)
	sel := p["select"].(map[string]any)
	opts, _ := sel["options"].([]any)
	if pref != "" {
		for _, it := range opts {
			if it.(map[string]any)["name"] == pref {
				return pref
			}
		}
	}
	if len(opts) > 0 {
		return fmt.Sprint(opts[0].(map[string]any)["name"])
	}
	return ""
}

func (r *Runner) chooseDifferentSelect(prop, current string) string {
	if !r.hasProp(prop, "select") {
		return ""
	}
	props, _ := r.dbJSON["properties"].(map[string]any)
	p := props[prop].(map[string]any)
	sel := p["select"].(map[string]any)
	opts, _ := sel["options"].([]any)
	for _, it := range opts {
		name := fmt.Sprint(it.(map[string]any)["name"])
		if name != current {
			return name
		}
	}
	return current
}

func (r *Runner) chooseStatusOption(pref string) string {
	if r.statusK == "" {
		return ""
	}
	props, _ := r.dbJSON["properties"].(map[string]any)
	p := props[r.statusK].(map[string]any)
	st := p["status"].(map[string]any)
	groups, _ := st["options"].([]any)
	// Some APIs return options flat; attempt both
	if len(groups) == 0 {
		if opts, ok := st["options"].([]any); ok && len(opts) > 0 {
			for _, it := range opts {
				if it.(map[string]any)["name"] == pref {
					return pref
				}
			}
			return fmt.Sprint(opts[0].(map[string]any)["name"])
		}
		return ""
	}
	for _, it := range groups {
		name := fmt.Sprint(it.(map[string]any)["name"])
		if name == pref {
			return pref
		}
	}
	name0 := fmt.Sprint(groups[0].(map[string]any)["name"])
	return name0
}

func (r *Runner) chooseDifferentStatus(current string) string {
	if r.statusK == "" {
		return ""
	}
	props, _ := r.dbJSON["properties"].(map[string]any)
	p := props[r.statusK].(map[string]any)
	st := p["status"].(map[string]any)
	opts, _ := st["options"].([]any)
	for _, it := range opts {
		name := fmt.Sprint(it.(map[string]any)["name"])
		if name != current {
			return name
		}
	}
	return current
}

// time helpers
func timeNow() string           { return time.Now().Format("20060102-150405") }
func todayPlus(days int) string { return time.Now().AddDate(0, 0, days).Format("2006-01-02") }

func (r *Runner) CleanupUntitled(ctx context.Context) error {
	// Ensure DB metadata is loaded to get the title property key
	if r.title == "" {
		if err := r.fetchDB(ctx); err != nil {
			return err
		}
	}
	r.log.Printf("Cleaning untitled tasks…")
	cursor := ""
	for i := 0; i < 50; i++ { // safety cap
		// Prefer property id if available to avoid name mismatches
		propIdent := r.title
		if r.titleID != "" {
			propIdent = r.titleID
		}
		q := map[string]any{
			"filter": map[string]any{
				"property": propIdent,
				"title":    map[string]any{"is_empty": true},
			},
			"page_size": 100,
		}
		if cursor != "" {
			q["start_cursor"] = cursor
		}
		body, _ := json.Marshal(q)
		b, code, err := r.c.QueryDatabase(r.dbID, body)
		if err != nil || code/100 != 2 {
			return fmt.Errorf("query untitled: %d %v %s", code, err, string(b))
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		results, _ := m["results"].([]any)
		for _, it := range results {
			row, _ := it.(map[string]any)
			id, _ := row["id"].(string)
			if id == "" {
				continue
			}
			payload := []byte(`{"archived":true}`)
			rb, rcode, rerr := r.c.UpdatePage(id, payload)
			if rerr != nil || rcode/100 != 2 {
				r.log.Printf("warn: archive %s: %d %v %s", id, rcode, rerr, string(rb))
			}
		}
		hasMore, _ := m["has_more"].(bool)
		if !hasMore {
			break
		}
		cursor, _ = m["next_cursor"].(string)
		if cursor == "" {
			break
		}
	}
	return nil
}

func (r *Runner) ensureTask(ctx context.Context, title string) (string, error) {
	if id := r.firstByTitle(ctx, title); id != "" {
		return id, nil
	}
	r.log.Printf("Creating task: %s", title)
	props := map[string]any{
		r.title: map[string]any{
			"title": []any{map[string]any{"text": map[string]any{"content": title}}},
		},
	}
	if r.goalID != "" && r.hasProp("Goals", "relation") {
		props["Goals"] = map[string]any{
			"relation": []any{map[string]any{"id": r.goalID}},
		}
	}
	payload := map[string]any{
		"parent":     map[string]any{"database_id": r.dbID},
		"properties": props,
	}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create page: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	id, _ := m["id"].(string)
	return id, nil
}

func (r *Runner) firstByTitle(ctx context.Context, title string) string {
	q := map[string]any{
		"filter": map[string]any{
			"property": r.title,
			"title":    map[string]any{"equals": title},
		},
		"page_size": 1,
	}
	body, _ := json.Marshal(q)
	b, code, err := r.c.QueryDatabase(r.dbID, body)
	if err != nil || code/100 != 2 {
		r.log.Printf("query title %q: %d %v %s", title, code, err, string(b))
		return ""
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	if len(results) == 0 {
		return ""
	}
	row, _ := results[0].(map[string]any)
	id, _ := row["id"].(string)
	return id
}

func (r *Runner) hasProp(name, typ string) bool {
	props, _ := r.dbJSON["properties"].(map[string]any)
	p, ok := props[name]
	if !ok {
		return false
	}
	m, _ := p.(map[string]any)
	if typ == "" {
		return true
	}
	return strings.EqualFold(fmt.Sprint(m["type"]), typ)
}

func (r *Runner) setParent(ctx context.Context, child, parent string) error {
	if r.relSelf == "" {
		return nil
	}
	props := map[string]any{
		r.relSelf: map[string]any{"relation": []any{map[string]any{"id": parent}}},
	}
	payload := map[string]any{"properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.UpdatePage(child, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("set parent: %d %v %s", code, err, string(b))
	}
	return nil
}

// SetParent exported: set self-relation parent if available
func (r *Runner) SetParent(ctx context.Context, child, parent string) error {
	return r.setParent(ctx, child, parent)
}

func (r *Runner) safeSetGoal(ctx context.Context, pageID string) error {
	if r.goalID == "" || !r.hasProp("Goals", "relation") {
		return nil
	}
	// sanity GET goal
	if b, code, _ := r.c.GetPage(r.goalID); code/100 != 2 {
		r.log.Printf("goal page not accessible: %s", string(b))
		return nil
	}
	props := map[string]any{
		"Goals": map[string]any{"relation": []any{map[string]any{"id": r.goalID}}},
	}
	payload := map[string]any{"properties": props}
	body, _ := json.Marshal(payload)
	b, code, err := r.c.UpdatePage(pageID, body)
	if err != nil || code/100 != 2 {
		r.log.Printf("warn: set goal: %d %v %s", code, err, string(b))
	}
	return nil
}
