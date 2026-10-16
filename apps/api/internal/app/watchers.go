package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/logctx"
)

// Watchers poll external sources and turn new entries into pending items in a
// collection, so they flow through rules, the inbox and webhooks like any
// other item. The first run only records what is already there (no flood).

const (
	WatcherRSS     = "rss"
	WatcherURLDiff = "url_diff"

	watcherFailThreshold = 5              // consecutive failures → degraded
	watcherPauseAfter    = 24 * time.Hour // degraded this long → paused
	watcherSeenRetention = 90 * 24 * time.Hour
	maxNewItemsPerRun    = 20
)

type Watcher struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	URL             string   `json:"url"`
	CollectionID    string   `json:"collection_id"`  // the first of CollectionIDs
	CollectionIDs   []string `json:"collection_ids"` // every collection new entries are added to
	Keywords        []string `json:"keywords"`
	ExcludeKeywords []string `json:"exclude_keywords"`
	Every           string   `json:"every"`
	EverySeconds    int64    `json:"every_seconds"`
	Status          string   `json:"status"`
	LastError       string   `json:"last_error"`
	LastRunAt       *string  `json:"last_run_at"`
	NextRunAt       string   `json:"next_run_at"`
	ItemsCreated    int64    `json:"items_created"`
	FailCount       int64    `json:"fail_count"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

func toWatcher(w db.Watcher, cols []string) Watcher {
	if len(cols) == 0 {
		cols = []string{w.CollectionID}
	}
	out := Watcher{ID: w.ID, Name: w.Name, Kind: w.Kind, URL: w.Url, CollectionID: w.CollectionID, CollectionIDs: cols, EverySeconds: w.EverySeconds,
		Every: (time.Duration(w.EverySeconds) * time.Second).String(), Status: w.Status, LastError: w.LastError,
		LastRunAt: ptr(w.LastRunAt), NextRunAt: w.NextRunAt, ItemsCreated: w.ItemsCreated, FailCount: w.FailCount,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	_ = json.Unmarshal([]byte(w.Keywords), &out.Keywords)
	_ = json.Unmarshal([]byte(w.ExcludeKeywords), &out.ExcludeKeywords)
	return out
}

type WatcherInput struct {
	Name            *string   `json:"name"`
	Kind            *string   `json:"kind"`
	URL             *string   `json:"url"`
	Collection      *string   `json:"collection"`  // ID or name
	Collections     *[]string `json:"collections"` // IDs or names: new entries go to each (replaces collection)
	Keywords        *[]string `json:"keywords"`
	ExcludeKeywords *[]string `json:"exclude_keywords"`
	Every           *string   `json:"every"`  // Go duration: "15m", "2h"
	Status          *string   `json:"status"` // active | paused (resume / pause)
}

func cleanWords(in []string, field string) ([]string, error) {
	out := []string{}
	for _, w := range in {
		if w = strings.TrimSpace(w); w != "" {
			if utf8.RuneCountInString(w) > 100 {
				return nil, invalid(field, "words must be at most 100 characters")
			}
			out = append(out, w)
		}
	}
	if len(out) > 50 {
		return nil, invalid(field, "at most 50 words")
	}
	return out, nil
}

// watcherCollections lists the collections a watcher adds to, in order (the first is w.CollectionID).
func (s *Service) watcherCollections(ctx context.Context, q *db.Queries, w db.Watcher) ([]string, error) {
	cols, err := q.WatcherCollections(ctx, w.ID)
	if len(cols) == 0 {
		cols = []string{w.CollectionID}
	}
	return cols, err
}

func (s *Service) toWatcher(ctx context.Context, w db.Watcher) (Watcher, error) {
	cols, err := s.watcherCollections(ctx, s.Q, w)
	return toWatcher(w, cols), err
}

func (s *Service) setWatcherCollections(ctx context.Context, q *db.Queries, id string, cols []string) error {
	if err := q.ClearWatcherCollections(ctx, id); err != nil {
		return err
	}
	for _, c := range cols {
		if err := q.AddWatcherCollection(ctx, db.AddWatcherCollectionParams{WatcherID: id, CollectionID: c}); err != nil {
			return err
		}
	}
	return nil
}

// applyWatcherInput changes w; cols is the new collection list when the input sets one, else nil.
func (s *Service) applyWatcherInput(ctx context.Context, p Principal, plan Plan, w *db.Watcher, in WatcherInput) (cols []string, err error) {
	if in.Collection != nil && in.Collections == nil {
		in.Collections = &[]string{*in.Collection}
	}
	if in.Collections != nil {
		if len(*in.Collections) == 0 || len(*in.Collections) > 20 {
			return nil, invalid("collections", "pick 1 to 20 collections")
		}
		for _, ref := range *in.Collections {
			c, err := s.FindCollection(ctx, p, ref)
			if err != nil {
				return nil, invalid("collections", "unknown collection: "+ref)
			}
			if !slices.Contains(cols, c.ID) {
				cols = append(cols, c.ID)
			}
		}
		w.CollectionID = cols[0]
	}
	return cols, s.applyWatcherFields(ctx, plan, w, in)
}

func (s *Service) applyWatcherFields(ctx context.Context, plan Plan, w *db.Watcher, in WatcherInput) error {
	var err error
	if in.Name != nil {
		if w.Name, err = trimmed(*in.Name, "name", 100, true); err != nil {
			return err
		}
	}
	if in.Kind != nil {
		if *in.Kind != WatcherRSS && *in.Kind != WatcherURLDiff {
			return invalid("kind", "must be rss or url_diff")
		}
		w.Kind = *in.Kind
	}
	if in.URL != nil {
		u, err := url.Parse(strings.TrimSpace(*in.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(*in.URL) > 2000 {
			return invalid("url", "must be an absolute http(s) URL without credentials")
		}
		w.Url = u.String()
	}
	if in.Keywords != nil {
		k, err := cleanWords(*in.Keywords, "keywords")
		if err != nil {
			return err
		}
		w.Keywords = mustJSON(k)
	}
	if in.ExcludeKeywords != nil {
		k, err := cleanWords(*in.ExcludeKeywords, "exclude_keywords")
		if err != nil {
			return err
		}
		w.ExcludeKeywords = mustJSON(k)
	}
	if in.Every != nil {
		d, err := time.ParseDuration(*in.Every)
		if err != nil || d > 7*24*time.Hour {
			return invalid("every", `must be a duration like "15m" or "6h", at most 168h`)
		}
		min := time.Duration(plan.Limits.MinWatcherIntervalSeconds) * time.Second
		if d < min {
			e := limitErr(plan, LimitWatcherEvery, plan.Limits.MinWatcherIntervalSeconds, int64(d.Seconds()))
			e.Message = fmt.Sprintf("The %s plan checks sources at most every %s.", plan.Name, formatTimeout(min))
			return e
		}
		w.EverySeconds = int64(d.Seconds())
	}
	if in.Status != nil {
		switch *in.Status {
		case "active":
			w.Status, w.FailCount, w.DegradedSince = "active", 0, nullStr("")
			w.NextRunAt = s.now()
		case "paused":
			w.Status = "paused"
		default:
			return invalid("status", "must be active or paused")
		}
	}
	if w.Kind == "" && w.Url != "" {
		if w.Kind, err = s.detectWatcherKind(ctx, w.Url); err != nil {
			return err
		}
	}
	if w.Kind == "" || w.Url == "" || w.CollectionID == "" || w.Name == "" {
		return invalid("watcher", "name, kind, url and collection are required")
	}
	return nil
}

// detectWatcherKind reads the URL once: an RSS/Atom feed is watched entry by entry (rss), anything else (a
// page, a JSON endpoint) for changes (url_diff). Callers that know better pass kind and skip this.
func (s *Service) detectWatcherKind(ctx context.Context, rawURL string) (string, error) {
	u, _ := url.Parse(rawURL)
	if !s.robotsAllowed(ctx, u) {
		return "", invalid("url", u.Host+" does not allow automated access (robots.txt)")
	}
	body, _, err := s.get(ctx, rawURL)
	if err != nil {
		return "", invalid("url", "could not read it ("+err.Error()+"); pass kind to create it anyway")
	}
	if isFeed(body) {
		return WatcherRSS, nil
	}
	return WatcherURLDiff, nil
}

func (s *Service) CreateWatcher(ctx context.Context, p Principal, in WatcherInput) (Watcher, error) {
	if err := p.require(ScopeWatchersManage); err != nil {
		return Watcher{}, err
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Watcher{}, err
	}
	now := s.now()
	w := db.Watcher{ID: newID("wat"), WorkspaceID: p.WorkspaceID, Keywords: "[]", ExcludeKeywords: "[]", Status: "active",
		EverySeconds: max(3600, plan.Limits.MinWatcherIntervalSeconds), NextRunAt: now, CreatedAt: now, UpdatedAt: now}
	if in.Status != nil {
		return Watcher{}, invalid("status", "new watchers start active")
	}
	cols, err := s.applyWatcherInput(ctx, p, plan, &w, in)
	if err != nil {
		return Watcher{}, err
	}
	err = s.tx(ctx, func(q *db.Queries) error {
		n, err := q.CountActiveWatchers(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := checkCount(plan, LimitWatchers, plan.Limits.Watchers, n); err != nil {
			return err
		}
		if err := q.CreateWatcher(ctx, db.CreateWatcherParams{ID: w.ID, WorkspaceID: w.WorkspaceID, CollectionID: w.CollectionID,
			Name: w.Name, Kind: w.Kind, Url: w.Url, Keywords: w.Keywords, ExcludeKeywords: w.ExcludeKeywords,
			EverySeconds: w.EverySeconds, NextRunAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		if err := s.setWatcherCollections(ctx, q, w.ID, cols); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "watcher.created", w.CollectionID, "", map[string]any{"watcher": toWatcher(w, cols)})
		return err
	})
	if err != nil {
		return Watcher{}, err
	}
	s.wakeWatchers()
	return toWatcher(w, cols), nil
}

func (s *Service) getWatcher(ctx context.Context, p Principal, id string) (db.Watcher, error) {
	if err := p.require(ScopeWatchersManage); err != nil {
		return db.Watcher{}, err
	}
	w, err := s.Q.GetWatcher(ctx, db.GetWatcherParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return w, notFound("watcher")
	}
	return w, err
}

func (s *Service) GetWatcher(ctx context.Context, p Principal, id string) (Watcher, error) {
	w, err := s.getWatcher(ctx, p, id)
	if err != nil {
		return Watcher{}, err
	}
	return s.toWatcher(ctx, w)
}

func (s *Service) ListWatchers(ctx context.Context, p Principal) ([]Watcher, error) {
	if err := p.require(ScopeWatchersManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWatchers(ctx, p.WorkspaceID)
	out := []Watcher{}
	for _, r := range rows {
		// ponytail: one query per watcher; a workspace has a handful
		w, err := s.toWatcher(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, err
}

func (s *Service) UpdateWatcher(ctx context.Context, p Principal, id string, in WatcherInput) (Watcher, error) {
	w, err := s.getWatcher(ctx, p, id)
	if err != nil {
		return Watcher{}, err
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Watcher{}, err
	}
	if in.Kind != nil && *in.Kind != w.Kind {
		return Watcher{}, invalid("kind", "cannot change; create a new watcher")
	}
	if in.Status != nil && *in.Status == w.Status && onlyChange(in, "Status") {
		return Watcher{}, alreadyErr("watcher", w.Status)
	}
	wasPaused := w.Status == "paused"
	cols, err := s.applyWatcherInput(ctx, p, plan, &w, in)
	if err != nil {
		return Watcher{}, err
	}
	w.UpdatedAt = s.now()
	err = s.tx(ctx, func(q *db.Queries) error {
		if wasPaused && w.Status != "paused" { // resuming counts against the plan again
			n, err := q.CountActiveWatchers(ctx, p.WorkspaceID)
			if err != nil {
				return err
			}
			if err := checkCount(plan, LimitWatchers, plan.Limits.Watchers, n); err != nil {
				return err
			}
		}
		if err := q.UpdateWatcher(ctx, db.UpdateWatcherParams{CollectionID: w.CollectionID, Name: w.Name, Url: w.Url, Keywords: w.Keywords,
			ExcludeKeywords: w.ExcludeKeywords, EverySeconds: w.EverySeconds, Status: w.Status, FailCount: w.FailCount,
			DegradedSince: w.DegradedSince, NextRunAt: w.NextRunAt, UpdatedAt: w.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		if cols != nil {
			if err := s.setWatcherCollections(ctx, q, w.ID, cols); err != nil {
				return err
			}
		} else if cols, err = s.watcherCollections(ctx, q, w); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "watcher.updated", w.CollectionID, "", map[string]any{"watcher": toWatcher(w, cols)})
		return err
	})
	s.wakeWatchers()
	return toWatcher(w, cols), err
}

func (s *Service) DeleteWatcher(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeWatchersManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.DeleteWatcher(ctx, db.DeleteWatcherParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err == nil && n == 0 {
			return notFound("watcher")
		}
		if err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "watcher.deleted", "", "", map[string]any{"watcher_id": id})
		return err
	})
}

type WatcherRun struct {
	Status   string `json:"status"`
	NewItems int    `json:"new_items"`
	Seen     int    `json:"entries_seen"`
	Baseline bool   `json:"baseline"`
	Error    string `json:"error,omitempty"`
}

// RunWatcherNow checks a watcher immediately (the "Check now" button).
func (s *Service) RunWatcherNow(ctx context.Context, p Principal, id string) (WatcherRun, error) {
	w, err := s.getWatcher(ctx, p, id)
	if err != nil {
		return WatcherRun{}, err
	}
	return s.processWatcher(ctx, w), nil
}

// ---- scheduler ----

func (s *Service) wakeWatchers() {
	select {
	case s.watcherWake <- struct{}{}:
	default:
	}
}

// RunWatchers runs due watchers until ctx is cancelled.
func (s *Service) RunWatchers(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	defer wg.Wait()
	prune := time.Now()
	for {
		due, err := s.Q.DueWatchers(ctx, db.DueWatchersParams{NextRunAt: s.now(), Limit: 20})
		if err != nil && ctx.Err() == nil {
			s.logger(ctx).Error("watcher poll failed", "err", err)
		}
		for _, w := range due {
			// Lease: push next_run_at out so a crash mid-fetch just delays the run.
			n, err := s.W.LeaseWatcher(ctx, db.LeaseWatcherParams{LeaseUntil: formatTime(s.Now().Add(10 * time.Minute)), ID: w.ID, Expected: w.NextRunAt})
			if err != nil || n == 0 {
				continue
			}
			// Suspended (unpaid Pro) workspaces create nothing; the lease
			// retries the watcher in 10 minutes.
			if s.CheckWritable(ctx, w.WorkspaceID) != nil {
				continue
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Add(1)
			go func(w db.Watcher) {
				defer func() { <-sem; wg.Done() }()
				s.processWatcher(context.WithoutCancel(ctx), w)
			}(w)
		}
		if time.Since(prune) > time.Hour {
			_ = s.W.PruneSeen(ctx, formatTime(s.Now().Add(-watcherSeenRetention)))
			prune = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.watcherWake:
		}
	}
}

func (s *Service) nextRun(every int64) string {
	jitter := time.Duration(rand.Int64N(max(1, min(every/10, 60)))) * time.Second
	return formatTime(s.Now().Add(time.Duration(every)*time.Second + jitter))
}

func (s *Service) processWatcher(ctx context.Context, w db.Watcher) WatcherRun {
	ctx = logctx.With(ctx, "workspace_id", w.WorkspaceID, "watcher_id", w.ID)
	run := WatcherRun{Status: "ok", Baseline: w.BaselineDone == 0}
	newHash := w.ContentHash
	var keywords, exclude []string
	_ = json.Unmarshal([]byte(w.Keywords), &keywords)
	_ = json.Unmarshal([]byte(w.ExcludeKeywords), &exclude)

	entries, hash, err := s.fetchEntries(ctx, w)
	if err == nil {
		newHash = hash
		run.Seen = len(entries)
		run.NewItems, err = s.publishEntries(ctx, w, entries, keywords, exclude, run.Baseline)
	}

	now := s.now()
	upd := db.FinishWatcherRunParams{ID: w.ID, Status: w.Status, BaselineDone: w.BaselineDone, ContentHash: newHash,
		FailCount: 0, DegradedSince: nullStr(""), LastRunAt: nullStr(now), NextRunAt: s.nextRun(w.EverySeconds),
		ItemsCreated: int64(run.NewItems), UpdatedAt: now}
	if err != nil {
		run.Status, run.Error = "error", err.Error()
		upd.LastError, upd.FailCount, upd.DegradedSince = err.Error(), w.FailCount+1, w.DegradedSince
		switch {
		case w.Status == "active" && upd.FailCount >= watcherFailThreshold:
			upd.Status, upd.DegradedSince = "degraded", nullStr(now)
		case w.Status == "degraded" && w.DegradedSince.Valid:
			if since, perr := time.Parse(timeFormat, w.DegradedSince.String); perr == nil && s.Now().Sub(since) > watcherPauseAfter {
				upd.Status = "paused"
			}
		}
	} else {
		upd.Status, upd.BaselineDone = "active", 1
	}
	if w.Status == "paused" {
		upd.Status = "paused" // a manual "check now" doesn't resume a paused watcher
	}
	if ferr := s.tx(ctx, func(q *db.Queries) error {
		if err := q.FinishWatcherRun(ctx, upd); err != nil {
			return err
		}
		if upd.Status == w.Status {
			return nil
		}
		typ := "watcher." + upd.Status // degraded or paused
		if upd.Status == "active" {
			typ = "watcher.recovered"
		}
		actor := Principal{WorkspaceID: w.WorkspaceID, Kind: PrincipalSystem, ID: "watcher", Name: "Watcher"}
		_, err := s.emit(ctx, q, actor, typ, w.CollectionID, "", map[string]any{"watcher_id": w.ID, "name": w.Name,
			"from": w.Status, "to": upd.Status, "error": upd.LastError})
		return err
	}); ferr != nil {
		s.logger(ctx).Error("watcher state update failed", "err", ferr)
	}
	metricWatcherRuns.Inc(w.Kind, run.Status)
	metricWatcherItems.Add(float64(run.NewItems), w.Kind)
	s.logger(ctx).Info("watcher run", "kind", w.Kind, "status", run.Status, "new_items", run.NewItems, "baseline", run.Baseline)
	return run
}

// fetchEntries returns the source's entries (rss) or a single synthetic entry
// when the page changed (url_diff), plus the page hash for url_diff.
func (s *Service) fetchEntries(ctx context.Context, w db.Watcher) ([]feedEntry, string, error) {
	u, err := url.Parse(w.Url)
	if err != nil {
		return nil, "", err
	}
	// robots.txt applies to feeds too: a site that disallows automated access
	// (e.g. Reddit) is not watched, whatever URL points at it.
	if !s.robotsAllowed(ctx, u) {
		return nil, w.ContentHash, fmt.Errorf("robots_disallowed: %s does not allow automated access to this page", u.Host)
	}
	body, _, err := s.get(ctx, w.Url)
	if err != nil {
		return nil, "", err
	}
	if w.Kind == WatcherRSS {
		entries, err := parseFeed(body)
		return entries, "", err
	}
	text := cleanText(string(body), 200_000)
	hash := sha(text)
	if hash == w.ContentHash || w.BaselineDone == 0 {
		return nil, hash, nil
	}
	title := firstNonEmpty(pageTitle(body), u.Host+u.Path)
	return []feedEntry{{ID: hash, Title: "Changed: " + title, URL: w.Url, Summary: cleanText(text, 1500)}}, hash, nil
}

func (s *Service) publishEntries(ctx context.Context, w db.Watcher, entries []feedEntry, keywords, exclude []string, baseline bool) (int, error) {
	// Oldest first, so items appear in publication order.
	sort.SliceStable(entries, func(i, j int) bool { return i > j })
	p := Principal{WorkspaceID: w.WorkspaceID, Kind: PrincipalSystem, ID: w.ID, Name: "Watcher: " + w.Name,
		Scopes: []string{ScopeCollectionsRead, ScopeItemsWrite}}
	cols, err := s.watcherCollections(ctx, s.Q, w)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		if !baseline && created >= maxNewItemsPerRun {
			break // leave the rest unseen: the next run picks them up
		}
		seen := db.MarkSeenParams{WatcherID: w.ID, EntryHash: sha(e.ID), SeenAt: s.now()}
		if baseline || !keywordFilter(e.Title+" "+e.Summary, keywords, exclude) {
			if _, err := s.W.MarkSeen(ctx, seen); err != nil {
				return created, err
			}
			continue
		}
		in := CreateItemInput{Type: "link", Source: "watcher:" + w.Name, ExternalRef: sha(e.ID)[:16]}
		title, content := firstNonEmpty(e.Title, e.URL, "Untitled entry"), e.Summary
		in.Title, in.Content = &title, mustRaw(content)
		if e.URL != "" && (strings.HasPrefix(e.URL, "http://") || strings.HasPrefix(e.URL, "https://")) {
			in.URL = &e.URL
		} else {
			in.Type = "text"
		}
		meta := map[string]any{"untrusted": true, "watcher": map[string]string{"id": w.ID, "name": w.Name, "kind": w.Kind, "source_url": w.Url}}
		if e.Published != "" {
			meta["published_at"] = e.Published
		}
		if e.Author != "" {
			meta["author"] = e.Author
		}
		in.Metadata = json.RawMessage(mustJSON(meta))
		// The seen mark and the first collection's item commit together: if creating the item fails (e.g.
		// quota), the entry stays unseen and is retried next run.
		// ponytail: the other collections get theirs after that commit; one failing there misses the entry
		// (the error shows on the watcher). Per-collection seen marks if that matters.
		_, err := s.createItem(ctx, p, cols[0], in, func(q *db.Queries) (bool, error) {
			fresh, err := q.MarkSeen(ctx, seen)
			return fresh > 0, err
		})
		if errors.Is(err, errSkipItem) {
			continue
		}
		if err != nil {
			return created, err // e.g. plan quota reached: surfaced as the watcher's error
		}
		created++
		for _, c := range cols[1:] {
			if _, err := s.createItem(ctx, p, c, in, nil); err != nil {
				return created, err
			}
			created++
		}
	}
	return created, nil
}

func mustRaw(s string) json.RawMessage { return json.RawMessage(mustJSON(s)) }
