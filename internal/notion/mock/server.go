package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu        sync.Mutex
	databases map[string]*database
	pages     map[string]*Page
	pagesByDB map[string][]*Page
	pageSeq   int
	dbSeq     int
	propSeq   int
}

type database struct {
	ID          string
	JSON        map[string]any
	propByName  map[string]*propMeta
	propByID    map[string]*propMeta
	titlePropID string
}

type propMeta struct {
	Name string
	ID   string
	Type string
}

func (s *Server) nextPageID() string {
	s.pageSeq++
	return fmt.Sprintf("mock-page-%d", s.pageSeq)
}

func (s *Server) nextDatabaseID() string {
	s.dbSeq++
	return fmt.Sprintf("mock-db-%d", s.dbSeq)
}

func (s *Server) nextPropertyID() string {
	s.propSeq++
	return fmt.Sprintf("prop-%d", s.propSeq)
}

// Page represents minimal Notion page shape used in tests.
type Page struct {
	ID         string
	DatabaseID string
	Properties map[string]any
	Archived   bool
	Icon       map[string]any
	ParentType string
	ParentID   string
}

// NewServer spins up an httptest server that mimics the Notion API endpoints
// used by the CLI. Close() must be called when finished.
func NewServer() *Server {
	s := &Server{
		databases: make(map[string]*database),
		pages:     make(map[string]*Page),
		pagesByDB: make(map[string][]*Page),
	}
	return s
}

// Close shuts down the underlying httptest server.
func (s *Server) Close() {
}

// URL returns the base URL of the mock server (use with Client.SetBaseURL).
func (s *Server) URL() string {
	return "https://mock.local"
}

// RoundTrip implements http.RoundTripper so the server can be plugged directly
// into an http.Client transport without binding to a real network port.
func (s *Server) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	s.handle(recorder, req.Clone(req.Context()))
	return recorder.Result(), nil
}

// AddDatabase registers a database JSON payload (Notion format).
func (s *Server) AddDatabase(dbJSON map[string]any) {
	id, _ := dbJSON["id"].(string)
	if id == "" {
		panic("mock database requires an id")
	}
	props, _ := dbJSON["properties"].(map[string]any)
	propByName := make(map[string]*propMeta)
	propByID := make(map[string]*propMeta)
	var titlePropID string
	for name, raw := range props {
		m, _ := raw.(map[string]any)
		propID, _ := m["id"].(string)
		typeStr, _ := m["type"].(string)
		prop := &propMeta{Name: name, ID: propID, Type: typeStr}
		propByName[name] = prop
		if propID != "" {
			propByID[propID] = prop
		}
		if typeStr == "title" && propID != "" {
			titlePropID = propID
		}
	}
	s.mu.Lock()
	s.databases[id] = &database{
		ID:          id,
		JSON:        dbJSON,
		propByName:  propByName,
		propByID:    propByID,
		titlePropID: titlePropID,
	}
	s.mu.Unlock()
}

// AddPage registers (or replaces) a page instance.
func (s *Server) AddPage(p *Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = s.nextPageID()
	}
	if p.Properties == nil {
		p.Properties = make(map[string]any)
	}
	clone := clonePage(p)
	s.pages[p.ID] = clone
	if p.DatabaseID != "" {
		pages := s.pagesByDB[p.DatabaseID]
		filtered := pages[:0]
		for _, existing := range pages {
			if existing.ID != p.ID {
				filtered = append(filtered, existing)
			}
		}
		s.pagesByDB[p.DatabaseID] = append(filtered, clone)
	}
}

// Page returns a copy of the page if it exists.
func (s *Server) Page(id string) (*Page, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pages[id]
	if !ok {
		return nil, false
	}
	return clonePage(p), true
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/v1/databases":
		s.handleCreateDatabase(w, r)
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/v1/databases/"):
		id := strings.TrimPrefix(path, "/v1/databases/")
		s.handleUpdateDatabase(w, r, id)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/databases/"):
		id := strings.TrimPrefix(path, "/v1/databases/")
		s.handleGetDatabase(w, id)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/databases/") && strings.HasSuffix(path, "/query"):
		trimmed := strings.TrimPrefix(path, "/v1/databases/")
		id := strings.TrimSuffix(trimmed, "/query")
		s.handleQueryDatabase(w, r, id)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/pages/"):
		id := strings.TrimPrefix(path, "/v1/pages/")
		s.handleGetPage(w, id)
	case r.Method == http.MethodPost && path == "/v1/pages":
		s.handleCreatePage(w, r)
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/v1/pages/"):
		id := strings.TrimPrefix(path, "/v1/pages/")
		s.handleUpdatePage(w, r, id)
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/v1/blocks/") && strings.HasSuffix(path, "/children"):
		s.writeJSON(w, http.StatusOK, map[string]any{"object": "list", "results": []any{}})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}
}

func (s *Server) handleGetDatabase(w http.ResponseWriter, id string) {
	s.mu.Lock()
	db, ok := s.databases[id]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"database not found"}`))
		return
	}
	s.writeJSON(w, http.StatusOK, db.JSON)
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	propsPayload, _ := payload["properties"].(map[string]any)
	if propsPayload == nil {
		propsPayload = map[string]any{}
	}
	titleArr, _ := payload["title"].([]any)
	parent, _ := payload["parent"].(map[string]any)
	titleClone, _ := cloneAny(titleArr).([]any)
	parentClone, _ := cloneAny(parent).(map[string]any)

	dbID := s.nextDatabaseID()
	storedProps := map[string]any{}
	propByName := make(map[string]*propMeta)
	propByID := make(map[string]*propMeta)
	var titlePropID string

	for name, raw := range propsPayload {
		propMap, _ := cloneAny(raw).(map[string]any)
		typ := inferPropType(propMap)
		propID := s.nextPropertyID()
		propMap["id"] = propID
		propMap["type"] = typ
		storedProps[name] = propMap
		meta := &propMeta{Name: name, ID: propID, Type: typ}
		propByName[name] = meta
		propByID[propID] = meta
		if typ == "title" {
			titlePropID = propID
		}
	}

	dbJSON := map[string]any{
		"object":     "database",
		"id":         dbID,
		"title":      titleClone,
		"parent":     parentClone,
		"properties": storedProps,
	}

	s.mu.Lock()
	s.databases[dbID] = &database{
		ID:          dbID,
		JSON:        dbJSON,
		propByName:  propByName,
		propByID:    propByID,
		titlePropID: titlePropID,
	}
	s.pagesByDB[dbID] = nil
	s.mu.Unlock()

	s.writeJSON(w, http.StatusOK, dbJSON)
}

func (s *Server) handleUpdateDatabase(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	propsPayload, _ := payload["properties"].(map[string]any)
	if propsPayload == nil {
		propsPayload = map[string]any{}
	}

	s.mu.Lock()
	db, ok := s.databases[id]
	if !ok {
		s.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"database not found"}`))
		return
	}
	for name, raw := range propsPayload {
		propMap, _ := cloneAny(raw).(map[string]any)
		typ := inferPropType(propMap)
		propID := s.nextPropertyID()
		if existing, found := db.propByName[name]; found {
			propID = existing.ID
			existing.Type = typ
		} else {
			meta := &propMeta{Name: name, ID: propID, Type: typ}
			db.propByName[name] = meta
			db.propByID[propID] = meta
			if typ == "title" {
				db.titlePropID = propID
			}
		}
		propMap["id"] = propID
		propMap["type"] = typ
		props := db.JSON["properties"].(map[string]any)
		props[name] = propMap
	}
	updated := cloneAny(db.JSON).(map[string]any)
	s.mu.Unlock()

	s.writeJSON(w, http.StatusOK, updated)
}

type queryRequest struct {
	Filter      map[string]any `json:"filter"`
	PageSize    int            `json:"page_size"`
	StartCursor string         `json:"start_cursor"`
}

func (s *Server) handleQueryDatabase(w http.ResponseWriter, r *http.Request, id string) {
	var req queryRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	s.mu.Lock()
	db, ok := s.databases[id]
	pages := append([]*Page(nil), s.pagesByDB[id]...)
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"database not found"}`))
		return
	}

	var results []map[string]any
	for _, p := range pages {
		if p.Archived {
			continue
		}
		if !matchesFilter(db, p, req.Filter) {
			continue
		}
		results = append(results, pageToAPI(p))
	}

	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > len(results) {
		pageSize = len(results)
	}
	resp := map[string]any{
		"object":      "list",
		"results":     results[:pageSize],
		"has_more":    len(results) > pageSize,
		"next_cursor": "",
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func matchesFilter(db *database, p *Page, filter map[string]any) bool {
	if filter == nil || len(filter) == 0 {
		return true
	}
	if andList, ok := filter["and"].([]any); ok {
		for _, item := range andList {
			cond, _ := item.(map[string]any)
			if !matchesFilter(db, p, cond) {
				return false
			}
		}
		return true
	}
	if orList, ok := filter["or"].([]any); ok {
		for _, item := range orList {
			cond, _ := item.(map[string]any)
			if matchesFilter(db, p, cond) {
				return true
			}
		}
		return false
	}
	propName := propertyNameFromFilter(db, filter)
	if propName == "" {
		return true
	}
	propVal := p.Properties[propName]
	if titleFilter, ok := filter["title"].(map[string]any); ok {
		return matchTitle(propVal, titleFilter)
	}
	if richFilter, ok := filter["rich_text"].(map[string]any); ok {
		return matchRichText(propVal, richFilter)
	}
	if statusFilter, ok := filter["status"].(map[string]any); ok {
		return matchStatus(propVal, statusFilter)
	}
	if selectFilter, ok := filter["select"].(map[string]any); ok {
		return matchSelect(propVal, selectFilter)
	}
	if relationFilter, ok := filter["relation"].(map[string]any); ok {
		return matchRelation(propVal, relationFilter)
	}
	if dateFilter, ok := filter["date"].(map[string]any); ok {
		return matchDate(propVal, dateFilter)
	}
	if checkboxFilter, ok := filter["checkbox"].(map[string]any); ok {
		return matchCheckbox(propVal, checkboxFilter)
	}
	return true
}

func propertyNameFromFilter(db *database, filter map[string]any) string {
	if filter == nil {
		return ""
	}
	if prop, ok := filter["property"].(string); ok {
		if meta, exists := db.propByName[prop]; exists {
			return meta.Name
		}
		if meta, exists := db.propByID[prop]; exists {
			return meta.Name
		}
	}
	return ""
}

func matchTitle(prop any, cond map[string]any) bool {
	value := strings.TrimSpace(readTitle(prop))
	for key, raw := range cond {
		switch key {
		case "equals":
			return value == fmt.Sprint(raw)
		case "does_not_equal":
			return value != fmt.Sprint(raw)
		case "contains":
			return strings.Contains(strings.ToLower(value), strings.ToLower(fmt.Sprint(raw)))
		case "does_not_contain":
			return !strings.Contains(strings.ToLower(value), strings.ToLower(fmt.Sprint(raw)))
		case "starts_with":
			return strings.HasPrefix(strings.ToLower(value), strings.ToLower(fmt.Sprint(raw)))
		case "ends_with":
			return strings.HasSuffix(strings.ToLower(value), strings.ToLower(fmt.Sprint(raw)))
		case "is_empty":
			return value == ""
		case "is_not_empty":
			return value != ""
		}
	}
	return true
}

func matchRichText(prop any, cond map[string]any) bool {
	value := strings.TrimSpace(readRichText(prop))
	for key, raw := range cond {
		switch key {
		case "contains":
			return strings.Contains(strings.ToLower(value), strings.ToLower(fmt.Sprint(raw)))
		case "equals":
			return value == fmt.Sprint(raw)
		case "is_empty":
			return value == ""
		case "is_not_empty":
			return value != ""
		}
	}
	return true
}

func matchStatus(prop any, cond map[string]any) bool {
	name := readStatusName(prop)
	for key, raw := range cond {
		target := fmt.Sprint(raw)
		switch key {
		case "equals":
			return name == target
		case "does_not_equal":
			return name != target
		case "is_empty":
			return name == ""
		case "is_not_empty":
			return name != ""
		}
	}
	return true
}

func matchSelect(prop any, cond map[string]any) bool {
	name := readSelectName(prop)
	for key, raw := range cond {
		target := fmt.Sprint(raw)
		switch key {
		case "equals":
			return name == target
		case "does_not_equal":
			return name != target
		case "is_empty":
			return name == ""
		case "is_not_empty":
			return name != ""
		}
	}
	return true
}

func matchRelation(prop any, cond map[string]any) bool {
	ids := relationIDs(prop)
	for key, raw := range cond {
		target := strings.ToLower(fmt.Sprint(raw))
		switch key {
		case "contains":
			return containsString(ids, target)
		case "does_not_contain":
			return !containsString(ids, target)
		case "is_empty":
			return len(ids) == 0
		case "is_not_empty":
			return len(ids) > 0
		}
	}
	return true
}

func matchDate(prop any, cond map[string]any) bool {
	val := dateValue(prop)
	for key, raw := range cond {
		target := fmt.Sprint(raw)
		switch key {
		case "equals":
			return val == target
		case "on_or_before":
			return compareDate(val, target) <= 0
		case "on_or_after":
			return compareDate(val, target) >= 0
		case "is_empty":
			return val == ""
		case "is_not_empty":
			return val != ""
		}
	}
	return true
}

func matchCheckbox(prop any, cond map[string]any) bool {
	m, _ := prop.(map[string]any)
	checked, _ := m["checkbox"].(bool)
	for key, raw := range cond {
		want, _ := raw.(bool)
		switch key {
		case "equals":
			return checked == want
		case "does_not_equal":
			return checked != want
		}
	}
	return true
}

func readTitle(v any) string {
	m, _ := v.(map[string]any)
	arr, _ := m["title"].([]any)
	if len(arr) == 0 {
		return ""
	}
	first, _ := arr[0].(map[string]any)
	if plain, ok := first["plain_text"].(string); ok && plain != "" {
		return plain
	}
	if text, ok := first["text"].(map[string]any); ok {
		if content, ok := text["content"].(string); ok {
			return content
		}
	}
	return ""
}

func readRichText(v any) string {
	m, _ := v.(map[string]any)
	arr, _ := m["rich_text"].([]any)
	if len(arr) == 0 {
		return ""
	}
	first, _ := arr[0].(map[string]any)
	if plain, ok := first["plain_text"].(string); ok {
		return plain
	}
	if text, ok := first["text"].(map[string]any); ok {
		if content, ok := text["content"].(string); ok {
			return content
		}
	}
	return ""
}

func readStatusName(v any) string {
	m, _ := v.(map[string]any)
	status, _ := m["status"].(map[string]any)
	name, _ := status["name"].(string)
	return name
}

func readSelectName(v any) string {
	m, _ := v.(map[string]any)
	sel, _ := m["select"].(map[string]any)
	name, _ := sel["name"].(string)
	return name
}

func relationIDs(v any) []string {
	m, _ := v.(map[string]any)
	arr, _ := m["relation"].([]any)
	ids := make([]string, 0, len(arr))
	for _, item := range arr {
		entry, _ := item.(map[string]any)
		id, _ := entry["id"].(string)
		if id != "" {
			ids = append(ids, strings.ToLower(id))
		}
	}
	return ids
}

func dateValue(v any) string {
	m, _ := v.(map[string]any)
	dt, _ := m["date"].(map[string]any)
	start, _ := dt["start"].(string)
	return start
}

func compareDate(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	ta, errA := time.Parse("2006-01-02", a)
	tb, errB := time.Parse("2006-01-02", b)
	if errA == nil && errB == nil {
		switch {
		case ta.Before(tb):
			return -1
		case ta.After(tb):
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

func isTitleEmpty(v any) bool {
	m, _ := v.(map[string]any)
	arr, _ := m["title"].([]any)
	if len(arr) == 0 {
		return true
	}
	first, _ := arr[0].(map[string]any)
	plain, _ := first["plain_text"].(string)
	plain = strings.TrimSpace(plain)
	if plain != "" {
		return false
	}
	text, _ := first["text"].(map[string]any)
	content, _ := text["content"].(string)
	return strings.TrimSpace(content) == ""
}

func (s *Server) handleGetPage(w http.ResponseWriter, id string) {
	s.mu.Lock()
	p, ok := s.pages[id]
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"page not found"}`))
		return
	}
	s.writeJSON(w, http.StatusOK, pageToAPI(p))
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	parent, _ := payload["parent"].(map[string]any)
	dbID, _ := parent["database_id"].(string)
	workspaceParent, _ := parent["workspace"].(bool)
	pageParent, _ := parent["page_id"].(string)
	props, _ := payload["properties"].(map[string]any)
	icon, _ := payload["icon"].(map[string]any)

	page := &Page{
		DatabaseID: dbID,
		Properties: make(map[string]any),
		Icon:       icon,
	}
	if dbID != "" {
		page.ParentType = "database_id"
		page.ParentID = dbID
	} else if pageParent != "" {
		page.ParentType = "page_id"
		page.ParentID = pageParent
	} else if workspaceParent {
		page.ParentType = "workspace"
	}
	if dbID == "" && !workspaceParent && pageParent == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"missing parent"}`))
		return
	}
	for name, val := range props {
		page.Properties[name] = val
	}

	s.AddPage(page)
	s.writeJSON(w, http.StatusOK, pageToAPI(page))
}

func (s *Server) handleUpdatePage(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid json"}`))
		return
	}
	s.mu.Lock()
	page, ok := s.pages[id]
	if ok {
		if archived, has := payload["archived"].(bool); has {
			page.Archived = archived
		}
		if props, has := payload["properties"].(map[string]any); has {
			for name, val := range props {
				page.Properties[name] = val
			}
		}
		s.pages[id] = clonePage(page)
		pages := s.pagesByDB[page.DatabaseID]
		for i, existing := range pages {
			if existing.ID == id {
				pages[i] = clonePage(page)
			}
		}
		s.pagesByDB[page.DatabaseID] = pages
	}
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"page not found"}`))
		return
	}
	s.writeJSON(w, http.StatusOK, pageToAPI(page))
}

func pageToAPI(p *Page) map[string]any {
	parent := map[string]any{}
	switch p.ParentType {
	case "database_id":
		parent["type"] = "database_id"
		parent["database_id"] = p.ParentID
	case "page_id":
		parent["type"] = "page_id"
		parent["page_id"] = p.ParentID
	case "workspace":
		parent["type"] = "workspace"
		parent["workspace"] = true
	default:
		parent["type"] = "workspace"
		parent["workspace"] = true
	}
	return map[string]any{
		"object":     "page",
		"id":         p.ID,
		"archived":   p.Archived,
		"parent":     parent,
		"properties": p.Properties,
		"icon":       p.Icon,
	}
}

func clonePage(p *Page) *Page {
	if p == nil {
		return nil
	}
	iconAny := cloneAny(p.Icon)
	iconMap, _ := iconAny.(map[string]any)
	propsAny := cloneAny(p.Properties)
	propsMap, _ := propsAny.(map[string]any)
	if propsMap == nil {
		propsMap = make(map[string]any)
	}
	clone := &Page{
		ID:         p.ID,
		DatabaseID: p.DatabaseID,
		Archived:   p.Archived,
		Icon:       iconMap,
		Properties: propsMap,
		ParentType: p.ParentType,
		ParentID:   p.ParentID,
	}
	return clone
}

func inferPropType(prop map[string]any) string {
	if prop == nil {
		return "rich_text"
	}
	if t, ok := prop["type"].(string); ok && t != "" {
		return t
	}
	for k := range prop {
		switch k {
		case "title", "rich_text", "number", "select", "multi_select", "date", "relation", "status", "people", "files", "checkbox", "url", "email", "phone_number", "formula", "rollup", "created_time", "last_edited_time", "created_by", "last_edited_by":
			return k
		}
	}
	return "rich_text"
}

func cloneAny(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, vv := range val {
			out[k] = cloneAny(vv)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = cloneAny(vv)
		}
		return out
	default:
		return val
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}

// --- Helpers to build Notion-shaped JSON structures for tests ---

// StandardDatabase creates a Tasks-style database definition with title,
// status, select, date, and relation properties expected by the CLI.
func StandardDatabase(id string) map[string]any {
	return map[string]any{
		"object": "database",
		"id":     id,
		"properties": map[string]any{
			"Name": map[string]any{"id": "title", "type": "title", "title": map[string]any{}},
			"Status": map[string]any{
				"id":   "status",
				"type": "status",
				"status": map[string]any{
					"options": []any{
						map[string]any{"name": "Not Started"},
						map[string]any{"name": "In Progress"},
						map[string]any{"name": "Done"},
					},
				},
			},
			"Priority": map[string]any{
				"id":   "priority",
				"type": "select",
				"select": map[string]any{
					"options": []any{
						map[string]any{"name": "High"},
						map[string]any{"name": "Medium"},
						map[string]any{"name": "Low"},
					},
				},
			},
			"Do":  map[string]any{"id": "do", "type": "date", "date": map[string]any{}},
			"Due": map[string]any{"id": "due", "type": "date", "date": map[string]any{}},
			"Goals": map[string]any{
				"id":       "goals",
				"type":     "relation",
				"relation": map[string]any{"database_id": "goal-db"},
			},
			"Parent-task": map[string]any{
				"id":       "parent-task",
				"type":     "relation",
				"relation": map[string]any{"database_id": id},
			},
		},
	}
}

// Title returns a Notion title property.
func Title(value string) map[string]any {
	if strings.TrimSpace(value) == "" {
		return map[string]any{"title": []any{}}
	}
	return map[string]any{
		"title": []any{
			map[string]any{
				"plain_text": value,
				"text":       map[string]any{"content": value},
			},
		},
	}
}

// Status returns a Notion status property value.
func Status(name string) map[string]any {
	if name == "" {
		return map[string]any{"status": nil}
	}
	return map[string]any{"status": map[string]any{"name": name}}
}

// Select returns a Notion select property value.
func Select(name string) map[string]any {
	if name == "" {
		return map[string]any{"select": nil}
	}
	return map[string]any{"select": map[string]any{"name": name}}
}

// Date returns a Notion date property value.
func Date(value string) map[string]any {
	if value == "" {
		return map[string]any{"date": nil}
	}
	return map[string]any{"date": map[string]any{"start": value}}
}

// Relation returns a Notion relation property value.
func Relation(ids ...string) map[string]any {
	if len(ids) == 0 {
		return map[string]any{"relation": []any{}}
	}
	arr := make([]any, 0, len(ids))
	for _, id := range ids {
		arr = append(arr, map[string]any{"id": id})
	}
	return map[string]any{"relation": arr}
}

// PageWithProperties convenience helper to create a page.
func PageWithProperties(dbID, id string, props map[string]any) *Page {
	return &Page{ID: id, DatabaseID: dbID, Properties: props, ParentType: "database_id", ParentID: dbID}
}
