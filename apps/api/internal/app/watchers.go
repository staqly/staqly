package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
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
	CollectionID    string   `json:"collection_id"`
	Keywords        []string `json:"keywords"`
	ExcludeKeywords []string `json:"exclude_keywords"`
	Every           string   `json:"every"`
	EverySeconds    int64    `json:"every_seconds"`
	PresetID        string   `json:"preset_id,omitempty"`
	Status          string   `json:"status"`
	LastError       string   `json:"last_error"`
	LastRunAt       *string  `json:"last_run_at"`
	NextRunAt       string   `json:"next_run_at"`
	ItemsCreated    int64    `json:"items_created"`
	FailCount       int64    `json:"fail_count"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

func toWatcher(w db.Watcher) Watcher {
	out := Watcher{ID: w.ID, Name: w.Name, Kind: w.Kind, URL: w.Url, CollectionID: w.CollectionID, EverySeconds: w.EverySeconds,
		Every: (time.Duration(w.EverySeconds) * time.Second).String(), PresetID: w.PresetID, Status: w.Status, LastError: w.LastError,
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
	Collection      *string   `json:"collection"` // ID or name
	Keywords        *[]string `json:"keywords"`
	ExcludeKeywords *[]string `json:"exclude_keywords"`
	Every           *string   `json:"every"`  // Go duration: "15m", "2h"
	Status          *string   `json:"status"` // active | paused (resume / pause)
}

func cleanWords(in []string, field string) ([]string, error) {
	out := []string{}
	for _, w := range in {
		if w = strings.TrimSpace(w); w != "" {
			if len(w) > 100 {
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

func (s *Service) applyWatcherInput(ctx context.Context, p Principal, plan Plan, w *db.Watcher, in WatcherInput) error {
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
	if in.Collection != nil {
		c, err := s.FindCollection(ctx, p, *in.Collection)
		if err != nil {
			return invalid("collection", "unknown collection")
		}
		w.CollectionID = c.ID
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
			e.Message = fmt.Sprintf("The %s plan checks sources at most every %s.", plan.Name, min)
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
	if w.Kind == "" || w.Url == "" || w.CollectionID == "" || w.Name == "" {
		return invalid("watcher", "name, kind, url and collection are required")
	}
	return nil
}

func (s *Service) CreateWatcher(ctx context.Context, p Principal, in WatcherInput, presetID string) (Watcher, error) {
	if err := p.require(ScopeWatchersManage); err != nil {
		return Watcher{}, err
	}
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Watcher{}, err
	}
	now := s.now()
	w := db.Watcher{ID: newID("wat"), WorkspaceID: p.WorkspaceID, Keywords: "[]", ExcludeKeywords: "[]", Status: "active",
		EverySeconds: max(3600, plan.Limits.MinWatcherIntervalSeconds), PresetID: presetID, NextRunAt: now, CreatedAt: now, UpdatedAt: now}
	if in.Status != nil {
		return Watcher{}, invalid("status", "new watchers start active")
	}
	if err := s.applyWatcherInput(ctx, p, plan, &w, in); err != nil {
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
			EverySeconds: w.EverySeconds, PresetID: presetID, NextRunAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "watcher.created", w.CollectionID, "", map[string]any{"watcher": toWatcher(w)})
		return err
	})
	if err != nil {
		return Watcher{}, err
	}
	s.wakeWatchers()
	return toWatcher(w), nil
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
	return toWatcher(w), err
}

func (s *Service) ListWatchers(ctx context.Context, p Principal) ([]Watcher, error) {
	if err := p.require(ScopeWatchersManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListWatchers(ctx, p.WorkspaceID)
	out := []Watcher{}
	for _, r := range rows {
		out = append(out, toWatcher(r))
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
	wasPaused := w.Status == "paused"
	if err := s.applyWatcherInput(ctx, p, plan, &w, in); err != nil {
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
		return q.UpdateWatcher(ctx, db.UpdateWatcherParams{CollectionID: w.CollectionID, Name: w.Name, Url: w.Url, Keywords: w.Keywords,
			ExcludeKeywords: w.ExcludeKeywords, EverySeconds: w.EverySeconds, Status: w.Status, FailCount: w.FailCount,
			DegradedSince: w.DegradedSince, NextRunAt: w.NextRunAt, UpdatedAt: w.UpdatedAt, WorkspaceID: p.WorkspaceID, ID: id})
	})
	s.wakeWatchers()
	return toWatcher(w), err
}

func (s *Service) DeleteWatcher(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeWatchersManage); err != nil {
		return err
	}
	n, err := s.Q.DeleteWatcher(ctx, db.DeleteWatcherParams{WorkspaceID: p.WorkspaceID, ID: id})
	if err == nil && n == 0 {
		return notFound("watcher")
	}
	return err
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
			s.Log.Error("watcher poll failed", "err", err)
		}
		for _, w := range due {
			// Lease: push next_run_at out so a crash mid-fetch just delays the run.
			n, err := s.Q.LeaseWatcher(ctx, db.LeaseWatcherParams{LeaseUntil: formatTime(s.Now().Add(10 * time.Minute)), ID: w.ID, Expected: w.NextRunAt})
			if err != nil || n == 0 {
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
			_ = s.Q.PruneSeen(ctx, formatTime(s.Now().Add(-watcherSeenRetention)))
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
	if ferr := s.Q.FinishWatcherRun(ctx, upd); ferr != nil {
		s.Log.Error("watcher state update failed", "watcher_id", w.ID, "err", ferr)
	}
	s.Log.Info("watcher run", "watcher_id", w.ID, "kind", w.Kind, "status", run.Status, "new_items", run.NewItems, "baseline", run.Baseline)
	return run
}

// fetchEntries returns the source's entries (rss) or a single synthetic entry
// when the page changed (url_diff), plus the page hash for url_diff.
func (s *Service) fetchEntries(ctx context.Context, w db.Watcher) ([]feedEntry, string, error) {
	u, err := url.Parse(w.Url)
	if err != nil {
		return nil, "", err
	}
	// robots.txt governs crawling pages; feeds are published for machines
	// (and e.g. Reddit disallows everything, which would break its feeds).
	if w.Kind == WatcherURLDiff && !s.robotsAllowed(ctx, u) {
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
	created := 0
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		fresh, err := s.Q.MarkSeen(ctx, db.MarkSeenParams{WatcherID: w.ID, EntryHash: sha(e.ID), SeenAt: s.now()})
		if err != nil {
			return created, err
		}
		if fresh == 0 || baseline || created >= maxNewItemsPerRun || !keywordFilter(e.Title+" "+e.Summary, keywords, exclude) {
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
		if _, err := s.CreateItem(ctx, p, w.CollectionID, in); err != nil {
			return created, err // e.g. plan quota reached: surfaced as the watcher's error
		}
		created++
	}
	return created, nil
}

func mustRaw(s string) json.RawMessage { return json.RawMessage(mustJSON(s)) }

// ---- presets ----

type PresetParam struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Required    bool   `json:"required"`
	Default     string `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

type WatcherPreset struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Category    string        `json:"category"`
	Kind        string        `json:"kind"`
	Description string        `json:"description"`
	Every       string        `json:"default_every"`
	Params      []PresetParam `json:"params"`
	build       func(p map[string]string) (string, []string, error)
}

func q(s string) string { return url.QueryEscape(s) }

func req(p map[string]string, names ...string) error {
	for _, n := range names {
		if strings.TrimSpace(p[n]) == "" {
			return invalid("params."+n, "is required")
		}
	}
	return nil
}

func keywordsParam(p map[string]string) []string {
	var out []string
	for _, k := range strings.Split(p["keywords"], ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

var slugRe = func(s string) bool {
	for _, c := range s {
		if !(c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return s != ""
}

// WatcherPresets is the catalog of ready-made sources. Each preset expands to
// a plain rss or url_diff watcher.
var WatcherPresets = []WatcherPreset{
	{ID: "hn-front-page", Title: "Hacker News – Front page", Category: "Community", Kind: WatcherRSS, Every: "30m",
		Description: "Stories reaching the Hacker News front page.",
		build:       func(map[string]string) (string, []string, error) { return "https://hnrss.org/frontpage", nil, nil }},
	{ID: "hn-keyword", Title: "Hacker News – Keyword", Category: "Community", Kind: WatcherRSS, Every: "30m",
		Description: "New HN posts mentioning a keyword.",
		Params:      []PresetParam{{Name: "keyword", Label: "Keyword", Required: true}, {Name: "min_points", Label: "Minimum points", Default: "10"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "keyword"); err != nil {
				return "", nil, err
			}
			pts := firstNonEmpty(p["min_points"], "10")
			return "https://hnrss.org/newest?q=" + q(p["keyword"]) + "&points=" + q(pts), nil, nil
		}},
	{ID: "reddit-subreddit", Title: "Reddit – Subreddit", Category: "Community", Kind: WatcherRSS, Every: "30m",
		Description: "New posts in a subreddit.",
		Params:      []PresetParam{{Name: "subreddit", Label: "Subreddit", Required: true, Placeholder: "midjourney"}},
		build: func(p map[string]string) (string, []string, error) {
			sub := strings.TrimPrefix(strings.TrimSpace(p["subreddit"]), "r/")
			if !slugRe(sub) {
				return "", nil, invalid("params.subreddit", "is required (letters, digits, _)")
			}
			return "https://www.reddit.com/r/" + sub + "/new/.rss", nil, nil
		}},
	{ID: "reddit-keyword", Title: "Reddit – Keyword search", Category: "Community", Kind: WatcherRSS, Every: "30m",
		Description: "New Reddit posts matching a search, optionally within one subreddit.",
		Params:      []PresetParam{{Name: "query", Label: "Search", Required: true}, {Name: "subreddit", Label: "Subreddit (optional)"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "query"); err != nil {
				return "", nil, err
			}
			sub := strings.TrimPrefix(strings.TrimSpace(p["subreddit"]), "r/")
			if sub != "" && sub != "all" {
				if !slugRe(sub) {
					return "", nil, invalid("params.subreddit", "is invalid")
				}
				return "https://www.reddit.com/r/" + sub + "/search.rss?restrict_sr=1&sort=new&q=" + q(p["query"]), nil, nil
			}
			return "https://www.reddit.com/search.rss?sort=new&q=" + q(p["query"]), nil, nil
		}},
	{ID: "pinterest-board", Title: "Pinterest – Board", Category: "Inspiration", Kind: WatcherRSS, Every: "6h",
		Description: "New pins on a public Pinterest board — handy for visual inspiration.",
		Params:      []PresetParam{{Name: "user", Label: "User", Required: true}, {Name: "board", Label: "Board", Required: true}},
		build: func(p map[string]string) (string, []string, error) {
			if !slugRe(p["user"]) || !slugRe(p["board"]) {
				return "", nil, invalid("params", "user and board are required (as in the board URL)")
			}
			return "https://www.pinterest.com/" + p["user"] + "/" + p["board"] + ".rss", nil, nil
		}},
	{ID: "youtube-channel", Title: "YouTube – Channel", Category: "Content", Kind: WatcherRSS, Every: "6h",
		Description: "New videos from a YouTube channel.",
		Params:      []PresetParam{{Name: "channel_id", Label: "Channel ID", Required: true, Placeholder: "UC…"}},
		build: func(p map[string]string) (string, []string, error) {
			id := strings.TrimSpace(p["channel_id"])
			if !strings.HasPrefix(id, "UC") || len(id) != 24 || !slugRe(id) {
				return "", nil, invalid("params.channel_id", "must be a 24-character channel ID starting with UC")
			}
			return "https://www.youtube.com/feeds/videos.xml?channel_id=" + id, nil, nil
		}},
	{ID: "github-releases", Title: "GitHub – Releases", Category: "Developer", Kind: WatcherRSS, Every: "1h",
		Description: "New releases of a repository.",
		Params:      []PresetParam{{Name: "owner", Label: "Owner", Required: true}, {Name: "repo", Label: "Repository", Required: true}},
		build: func(p map[string]string) (string, []string, error) {
			if !slugRe(p["owner"]) || !slugRe(p["repo"]) {
				return "", nil, invalid("params", "owner and repo are required")
			}
			return "https://github.com/" + p["owner"] + "/" + p["repo"] + "/releases.atom", nil, nil
		}},
	{ID: "github-commits", Title: "GitHub – Commits", Category: "Developer", Kind: WatcherRSS, Every: "1h",
		Description: "New commits (or tags) on a repository's default branch.",
		Params:      []PresetParam{{Name: "owner", Label: "Owner", Required: true}, {Name: "repo", Label: "Repository", Required: true}, {Name: "feed", Label: "commits or tags", Default: "commits"}},
		build: func(p map[string]string) (string, []string, error) {
			if !slugRe(p["owner"]) || !slugRe(p["repo"]) {
				return "", nil, invalid("params", "owner and repo are required")
			}
			feed := firstNonEmpty(p["feed"], "commits")
			if feed != "commits" && feed != "tags" {
				return "", nil, invalid("params.feed", "must be commits or tags")
			}
			return "https://github.com/" + p["owner"] + "/" + p["repo"] + "/" + feed + ".atom", nil, nil
		}},
	{ID: "npm-package", Title: "npm – New version", Category: "Developer", Kind: WatcherURLDiff, Every: "6h",
		Description: "Fires when a package publishes a new latest version.",
		Params:      []PresetParam{{Name: "package", Label: "Package", Required: true, Placeholder: "@scope/name"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "package"); err != nil {
				return "", nil, err
			}
			return "https://registry.npmjs.org/" + strings.ReplaceAll(url.PathEscape(strings.TrimSpace(p["package"])), "%40", "@") + "/latest", nil, nil
		}},
	{ID: "pypi-package", Title: "PyPI – New release", Category: "Developer", Kind: WatcherRSS, Every: "6h",
		Description: "New releases of a Python package.",
		Params:      []PresetParam{{Name: "package", Label: "Package", Required: true}},
		build: func(p map[string]string) (string, []string, error) {
			name := strings.ToLower(strings.TrimSpace(p["package"]))
			if !slugRe(name) {
				return "", nil, invalid("params.package", "is required")
			}
			return "https://pypi.org/rss/project/" + name + "/releases.xml", nil, nil
		}},
	{ID: "stackoverflow-tag", Title: "Stack Overflow – Tag", Category: "Developer", Kind: WatcherRSS, Every: "30m",
		Description: "New questions with a tag.",
		Params:      []PresetParam{{Name: "tag", Label: "Tag", Required: true}},
		build: func(p map[string]string) (string, []string, error) {
			tag := strings.ToLower(strings.TrimSpace(p["tag"]))
			if tag == "" || len(tag) > 35 {
				return "", nil, invalid("params.tag", "is required (max 35 characters)")
			}
			return "https://stackoverflow.com/feeds/tag/" + url.PathEscape(tag), nil, nil
		}},
	{ID: "rss-feed", Title: "Any RSS / Atom feed", Category: "Content", Kind: WatcherRSS, Every: "1h",
		Description: "Any RSS or Atom feed URL.",
		Params:      []PresetParam{{Name: "url", Label: "Feed URL", Required: true}, {Name: "keywords", Label: "Keywords (comma-separated, optional)"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "url"); err != nil {
				return "", nil, err
			}
			return strings.TrimSpace(p["url"]), keywordsParam(p), nil
		}},
	{ID: "blog-newsletter", Title: "Blog / newsletter", Category: "Content", Kind: WatcherRSS, Every: "12h",
		Description: "Posts from a blog's feed, optionally only those matching keywords.",
		Params:      []PresetParam{{Name: "url", Label: "Feed URL", Required: true}, {Name: "keywords", Label: "Keywords (comma-separated, optional)"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "url"); err != nil {
				return "", nil, err
			}
			return strings.TrimSpace(p["url"]), keywordsParam(p), nil
		}},
	{ID: "arxiv-papers", Title: "arXiv – New papers", Category: "Research", Kind: WatcherRSS, Every: "6h",
		Description: "New papers in a category (e.g. cs.AI) or matching a search.",
		Params:      []PresetParam{{Name: "category", Label: "Category", Placeholder: "cs.AI"}, {Name: "query", Label: "Search"}},
		build: func(p map[string]string) (string, []string, error) {
			cat, query := strings.TrimSpace(p["category"]), strings.TrimSpace(p["query"])
			switch {
			case cat != "" && slugRe(cat):
				var kw []string
				if query != "" {
					kw = []string{query}
				}
				return "https://rss.arxiv.org/rss/" + cat, kw, nil
			case query != "":
				return "https://export.arxiv.org/api/query?sortBy=submittedDate&sortOrder=descending&max_results=30&search_query=all:" + q(query), nil, nil
			}
			return "", nil, invalid("params", "category or query is required")
		}},
	{ID: "google-news", Title: "Google News – Keyword", Category: "News", Kind: WatcherRSS, Every: "1h",
		Description: "News articles matching a search.",
		Params:      []PresetParam{{Name: "query", Label: "Search", Required: true}, {Name: "language", Label: "Language-country", Default: "en-US"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "query"); err != nil || len(p["query"]) > 500 {
				return "", nil, invalid("params.query", "is required (max 500 characters)")
			}
			lang, country, _ := strings.Cut(firstNonEmpty(p["language"], "en-US"), "-")
			if country == "" {
				country = strings.ToUpper(lang)
			}
			return "https://news.google.com/rss/search?q=" + q(p["query"]) + "&hl=" + q(lang) + "&gl=" + q(country) + "&ceid=" + q(country+":"+lang), nil, nil
		}},
	{ID: "product-hunt", Title: "Product Hunt – Daily", Category: "News", Kind: WatcherRSS, Every: "6h",
		Description: "New Product Hunt launches (high volume — add keywords).",
		Params:      []PresetParam{{Name: "keywords", Label: "Keywords (comma-separated, optional)"}},
		build: func(p map[string]string) (string, []string, error) {
			return "https://www.producthunt.com/feed", keywordsParam(p), nil
		}},
	{ID: "url-changed", Title: "Web page changed", Category: "Monitoring", Kind: WatcherURLDiff, Every: "6h",
		Description: "Fires when the visible text of a page changes.",
		Params:      []PresetParam{{Name: "url", Label: "Page URL", Required: true}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "url"); err != nil {
				return "", nil, err
			}
			return strings.TrimSpace(p["url"]), nil, nil
		}},
	{ID: "changelog-status", Title: "Changelog / status page", Category: "Monitoring", Kind: WatcherURLDiff, Every: "1h",
		Description: "Fires when a changelog or status page changes; add keywords like \"incident\".",
		Params:      []PresetParam{{Name: "url", Label: "Page URL", Required: true}, {Name: "keywords", Label: "Keywords (comma-separated, optional)"}},
		build: func(p map[string]string) (string, []string, error) {
			if err := req(p, "url"); err != nil {
				return "", nil, err
			}
			return strings.TrimSpace(p["url"]), keywordsParam(p), nil
		}},
}

type FromPresetInput struct {
	PresetID   string            `json:"preset_id"`
	Params     map[string]string `json:"params"`
	Collection string            `json:"collection"`
	Name       string            `json:"name,omitempty"`
	Every      string            `json:"every,omitempty"`
}

func (s *Service) CreateWatcherFromPreset(ctx context.Context, p Principal, in FromPresetInput) (Watcher, error) {
	i := slices.IndexFunc(WatcherPresets, func(w WatcherPreset) bool { return w.ID == in.PresetID })
	if i < 0 {
		return Watcher{}, invalid("preset_id", "unknown preset")
	}
	preset := WatcherPresets[i]
	if in.Params == nil {
		in.Params = map[string]string{}
	}
	u, kw, err := preset.build(in.Params)
	if err != nil {
		return Watcher{}, err
	}
	name := firstNonEmpty(in.Name, preset.Title)
	every := firstNonEmpty(in.Every, preset.Every)
	plan, err := s.planFor(ctx, s.Q, p.WorkspaceID)
	if err != nil {
		return Watcher{}, err
	}
	if d, err := time.ParseDuration(every); err == nil && in.Every == "" && int64(d.Seconds()) < plan.Limits.MinWatcherIntervalSeconds {
		every = (time.Duration(plan.Limits.MinWatcherIntervalSeconds) * time.Second).String()
	}
	if kw == nil {
		kw = []string{}
	}
	return s.CreateWatcher(ctx, p, WatcherInput{Name: &name, Kind: &preset.Kind, URL: &u, Collection: &in.Collection,
		Keywords: &kw, Every: &every}, preset.ID)
}
