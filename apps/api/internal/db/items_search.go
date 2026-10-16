package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Item listing is the one dynamic query (optional filters, full-text match,
// several sort orders, keyset pagination), so it is written by hand here
// instead of via sqlc. Only whitelisted SQL fragments are ever concatenated;
// all values are bound parameters.

type sortSpec struct {
	expr string // SQL expression; also the keyset value
	desc bool
}

var itemSorts = map[string]sortSpec{
	"created_desc": {"i.created_at", true},
	"created_asc":  {"i.created_at", false},
	"updated_desc": {"i.updated_at", true},
	"updated_asc":  {"i.updated_at", false},
	"decided_desc": {"COALESCE(i.decided_at, '')", true},
	"title_asc":    {"i.title COLLATE NOCASE", false},
	"relevance":    {"f.rank", false}, // bm25: lower is better
}

func ValidItemSort(s string) bool { _, ok := itemSorts[s]; return ok }

type ItemSearch struct {
	WorkspaceID                                                string
	CollectionID, Status, Type, Source, ExternalRef, RunAction string
	Archived                                                   string   // "false": leave archived items out; "true": only them
	Decisions, Runs, Tags                                      []string // any of these (empty = all)
	Query                                                      string   // user text, turned into an FTS5 prefix query
	CreatedAfter, CreatedBefore, DecidedAfter, DecidedBefore   string
	Sort                                                       string
	Cursor                                                     string // opaque, from a previous page
	Limit                                                      int
}

var ErrBadCursor = errors.New("invalid cursor")

// Search limits: a query is at most ftsMaxTerms distinct words and
// ftsMaxBytes of word text; the rest is ignored. Very short prefixes match a
// huge share of the index, so one-letter words are exact terms.
const (
	ftsMaxTerms = 16
	ftsMaxBytes = 256
)

// FTSQuery turns free text into a safe FTS5 query: every distinct word
// becomes a quoted prefix term (one-letter words: exact term) and all terms
// must match. Returns "" if there is nothing to search.
// The tokenizer strips diacritics (ó→o, ż→z), but "ł" is a separate letter in
// Unicode, so words typed with a plain "l" also try "ł" variants
// ("slonca" finds "słońca").
func FTSQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	terms := make([]string, 0, min(len(words), ftsMaxTerms))
	seen := map[string]bool{}
	size := 0
	for _, w := range words {
		w = strings.ToLower(w)
		if seen[w] {
			continue
		}
		if len(terms) == ftsMaxTerms || (len(terms) > 0 && size+len(w) > ftsMaxBytes) {
			break
		}
		for len(w) > ftsMaxBytes { // one very long word: search for its start
			_, n := utf8.DecodeLastRuneInString(w)
			w = w[:len(w)-n]
		}
		seen[w] = true
		size += len(w)
		star := "*"
		if utf8.RuneCountInString(w) == 1 {
			star = ""
		}
		vs := lVariants(w)
		for i, v := range vs {
			vs[i] = `"` + v + `"` + star
		}
		if len(vs) == 1 {
			terms = append(terms, vs[0])
		} else {
			terms = append(terms, "("+strings.Join(vs, " OR ")+")")
		}
	}
	// Explicit AND: FTS5 rejects an implicit AND after a parenthesised group.
	return strings.Join(terms, " AND ")
}

// lVariants returns w plus w with all "l"→"ł" and with each single "l"→"ł".
// ponytail: covers words with one or all-ł spellings; mixed multi-l words beyond that are missed.
func lVariants(w string) []string {
	r := []rune(w)
	out := []string{w}
	var pos []int
	for i, c := range r {
		if c == 'l' {
			pos = append(pos, i)
		}
	}
	if len(pos) == 0 || len(pos) > 4 {
		return out
	}
	all := append([]rune{}, r...)
	for _, i := range pos {
		all[i] = 'ł'
		if len(pos) > 1 {
			one := append([]rune{}, r...)
			one[i] = 'ł'
			out = append(out, string(one))
		}
	}
	return append(out, string(all))
}

type cursor struct {
	V  any    `json:"v"`
	ID string `json:"id"`
}

// SearchItems returns one page and the cursor for the next page ("" at the end).
func (q *Queries) SearchItems(ctx context.Context, s ItemSearch) ([]Item, string, error) {
	sort, ok := itemSorts[s.Sort]
	if !ok {
		return nil, "", errors.New("unknown sort")
	}
	match := FTSQuery(s.Query)
	if match == "" && strings.TrimSpace(s.Query) != "" {
		return nil, "", nil // only punctuation or symbols: nothing can match, rather than every item
	}
	if s.Sort == "relevance" && match == "" {
		sort = itemSorts["created_desc"]
	}
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`SELECT i.id, i.workspace_id, i.collection_id, i.title, i.type, i.content, i.url, i.media_id, i.metadata,
	i.status, i.source, i.external_ref, i.feedback, i.version, i.decided_at, i.created_at, i.updated_at,
	i.expires_at, i.editable, i.processing_timeout_seconds, i.decision, i.run, i.archived_at, i.run_action, i.tags, i.summary,
	` + sort.expr + ` AS sortv FROM items i `)
	if match != "" {
		sb.WriteString(`JOIN (SELECT rowid, bm25(items_fts, 10.0, 3.0, 1.0) AS rank FROM items_fts WHERE items_fts MATCH ?) f ON f.rowid = i.rowid `)
		args = append(args, match)
	}
	sb.WriteString(`WHERE i.workspace_id = ?`)
	args = append(args, s.WorkspaceID)
	for _, f := range []struct{ col, op, v string }{
		{"i.collection_id", "=", s.CollectionID}, {"i.status", "=", s.Status}, {"i.type", "=", s.Type},
		{"i.source", "=", s.Source}, {"i.external_ref", "=", s.ExternalRef},
		{"i.created_at", ">=", s.CreatedAfter}, {"i.created_at", "<", s.CreatedBefore},
		{"i.decided_at", ">=", s.DecidedAfter}, {"i.decided_at", "<", s.DecidedBefore},
	} {
		if f.v != "" {
			sb.WriteString(" AND " + f.col + " " + f.op + " ?")
			args = append(args, f.v)
		}
	}
	// Archived ends an item's story: it leaves every decision view and has its own, decision=archived.
	if len(s.Decisions) > 0 {
		var ds []string
		archived := false
		for _, d := range s.Decisions {
			if d == "archived" {
				archived = true
			} else {
				ds = append(ds, d)
			}
		}
		var or []string
		if len(ds) > 0 {
			or = append(or, "(i.archived_at IS NULL AND i.decision IN (?"+strings.Repeat(", ?", len(ds)-1)+"))")
			for _, d := range ds {
				args = append(args, d)
			}
		}
		if archived {
			or = append(or, "i.archived_at IS NOT NULL")
		}
		sb.WriteString(" AND (" + strings.Join(or, " OR ") + ")")
	}
	for _, f := range []struct {
		col  string
		vals []string
	}{{"i.run", s.Runs}} {
		if len(f.vals) > 0 {
			sb.WriteString(" AND " + f.col + " IN (?" + strings.Repeat(", ?", len(f.vals)-1) + ")")
			for _, v := range f.vals {
				args = append(args, v)
			}
		}
	}
	switch s.Archived {
	case "false":
		sb.WriteString(" AND i.archived_at IS NULL")
	case "true":
		sb.WriteString(" AND i.archived_at IS NOT NULL")
	}
	// run_action=none: runs no action started (a status set through the API).
	if s.RunAction == "none" {
		sb.WriteString(" AND COALESCE(i.run_action, '') = ''")
	} else if s.RunAction != "" {
		sb.WriteString(" AND i.run_action = ?")
		args = append(args, s.RunAction)
	}
	if len(s.Tags) > 0 {
		sb.WriteString(" AND EXISTS (SELECT 1 FROM json_each(i.tags) WHERE value IN (?" + strings.Repeat(", ?", len(s.Tags)-1) + "))")
		for _, t := range s.Tags {
			args = append(args, t)
		}
	}
	if s.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(s.Cursor)
		var c cursor
		if err != nil || json.Unmarshal(raw, &c) != nil || c.ID == "" {
			return nil, "", ErrBadCursor
		}
		switch c.V.(type) { // sort keys are text, numbers (rank) or NULL
		case nil, string, float64:
		default:
			return nil, "", ErrBadCursor
		}
		op := ">"
		if sort.desc {
			op = "<"
		}
		sb.WriteString(" AND (" + sort.expr + ", i.id) " + op + " (?, ?)")
		args = append(args, c.V, c.ID)
	}
	dir := " ASC"
	if sort.desc {
		dir = " DESC"
	}
	sb.WriteString(" ORDER BY " + sort.expr + dir + ", i.id" + dir + " LIMIT ?")
	args = append(args, s.Limit+1)

	rows, err := q.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var items []Item
	var lastV any
	more := false
	for rows.Next() {
		var i Item
		var v any
		if err := rows.Scan(&i.ID, &i.WorkspaceID, &i.CollectionID, &i.Title, &i.Type, &i.Content, &i.Url,
			&i.MediaID, &i.Metadata, &i.Status, &i.Source, &i.ExternalRef, &i.Feedback, &i.Version,
			&i.DecidedAt, &i.CreatedAt, &i.UpdatedAt, &i.ExpiresAt, &i.Editable, &i.ProcessingTimeoutSeconds,
			&i.Decision, &i.Run, &i.ArchivedAt, &i.RunAction, &i.Tags, &i.Summary, &v); err != nil {
			return nil, "", err
		}
		if len(items) == s.Limit {
			more = true // the extra row only tells us another page exists
			break
		}
		items = append(items, i)
		lastV = v
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if more {
		b, _ := json.Marshal(cursor{V: lastV, ID: items[len(items)-1].ID})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return items, next, nil
}
