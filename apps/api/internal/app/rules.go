package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/staqly/staqly/apps/api/internal/db"
)

// Rules decide what happens to a new item before anyone looks at it. They run
// once, when an item is created with status "pending", in priority order
// (lowest first); the first matching rule wins. No rules = every item waits
// in the inbox, exactly as before.

const (
	RuleAutoApprove = "auto_approve"
	RuleAutoReject  = "auto_reject"
	RuleSetStatus   = "set_status"
	RuleExpire      = "expire"
	RuleEscalate    = "escalate"
)

var ruleActions = []string{RuleAutoApprove, RuleAutoReject, RuleSetStatus, RuleExpire, RuleEscalate}

type RuleCondition struct {
	Path  string `json:"path"`  // dot path into item metadata, e.g. "score" or "post.likes"
	Op    string `json:"op"`    // eq ne lt lte gt gte contains in not_in exists
	Value any    `json:"value"` // omitted for exists
}

type RuleMatch struct {
	CollectionIDs []string        `json:"collection_ids,omitempty"`
	Types         []string        `json:"types,omitempty"`
	SourcePattern string          `json:"source_pattern,omitempty"` // glob: * and ?
	TextContains  string          `json:"text_contains,omitempty"`  // case-insensitive, title or content
	Conditions    []RuleCondition `json:"conditions,omitempty"`     // all must hold
}

type RuleParams struct {
	Feedback     string `json:"feedback,omitempty"`      // auto_reject / auto_approve note
	Status       string `json:"status,omitempty"`        // set_status
	AfterSeconds int64  `json:"after_seconds,omitempty"` // expire
	Note         string `json:"note,omitempty"`          // escalate
	ChannelID    string `json:"channel_id,omitempty"`    // escalate: notify only this channel
}

type Rule struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Priority     int64      `json:"priority"`
	Enabled      bool       `json:"enabled"`
	Match        RuleMatch  `json:"match"`
	Action       string     `json:"action"`
	Params       RuleParams `json:"params"`
	TimesApplied int64      `json:"times_applied"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

func toRule(r db.Rule) Rule {
	out := Rule{ID: r.ID, Name: r.Name, Priority: r.Priority, Enabled: r.Enabled == 1, Action: r.Action,
		TimesApplied: r.TimesApplied, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	_ = json.Unmarshal([]byte(r.Match), &out.Match)
	_ = json.Unmarshal([]byte(r.Params), &out.Params)
	return out
}

type RuleInput struct {
	Name     *string     `json:"name"`
	Priority *int64      `json:"priority"`
	Enabled  *bool       `json:"enabled"`
	Match    *RuleMatch  `json:"match"`
	Action   *string     `json:"action"`
	Params   *RuleParams `json:"params"`
}

var ruleOps = []string{"eq", "ne", "lt", "lte", "gt", "gte", "contains", "in", "not_in", "exists"}

func validateRule(r *Rule) error {
	var err error
	if r.Name, err = trimmed(r.Name, "name", 100, true); err != nil {
		return err
	}
	if r.Priority < 0 || r.Priority > 100000 {
		return invalid("priority", "must be between 0 and 100000")
	}
	if !slices.Contains(ruleActions, r.Action) {
		return invalid("action", "must be one of "+strings.Join(ruleActions, ", "))
	}
	for _, t := range r.Match.Types {
		if !slices.Contains(ItemTypes, t) {
			return invalid("match.types", "unknown type "+t)
		}
	}
	if len(r.Match.SourcePattern) > 200 || len(r.Match.TextContains) > 200 {
		return invalid("match", "patterns must be at most 200 characters")
	}
	if len(r.Match.Conditions) > 10 {
		return invalid("match.conditions", "at most 10 conditions")
	}
	for _, c := range r.Match.Conditions {
		if c.Path == "" || len(c.Path) > 100 {
			return invalid("match.conditions", "path is required (dot notation into metadata)")
		}
		if !slices.Contains(ruleOps, c.Op) {
			return invalid("match.conditions", "op must be one of "+strings.Join(ruleOps, ", "))
		}
		if _, isList := c.Value.([]any); (c.Op == "in" || c.Op == "not_in") && !isList {
			return invalid("match.conditions", c.Op+" needs an array value")
		}
	}
	switch r.Action {
	case RuleSetStatus:
		if !validStatus(r.Params.Status) || r.Params.Status == "approved" || r.Params.Status == "rejected" || r.Params.Status == "pending" {
			return invalid("params.status", "set_status needs a status other than pending/approved/rejected (use auto_approve/auto_reject)")
		}
	case RuleExpire:
		if r.Params.AfterSeconds < 60 || r.Params.AfterSeconds > 90*24*3600 {
			return invalid("params.after_seconds", "must be between 60 and 7776000 (90 days)")
		}
	}
	if len(r.Params.Feedback) > 2000 || len(r.Params.Note) > 2000 {
		return invalid("params", "texts must be at most 2000 characters")
	}
	return nil
}

func applyRuleInput(r *Rule, in RuleInput) {
	if in.Name != nil {
		r.Name = *in.Name
	}
	if in.Priority != nil {
		r.Priority = *in.Priority
	}
	if in.Enabled != nil {
		r.Enabled = *in.Enabled
	}
	if in.Match != nil {
		r.Match = *in.Match
	}
	if in.Action != nil {
		r.Action = *in.Action
	}
	if in.Params != nil {
		r.Params = *in.Params
	}
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (s *Service) CreateRule(ctx context.Context, p Principal, in RuleInput) (Rule, error) {
	if err := p.require(ScopeRulesManage); err != nil {
		return Rule{}, err
	}
	now := s.now()
	r := Rule{ID: newID("rul"), Priority: 100, Enabled: true, CreatedAt: now, UpdatedAt: now}
	applyRuleInput(&r, in)
	if err := validateRule(&r); err != nil {
		return Rule{}, err
	}
	err := s.tx(ctx, func(q *db.Queries) error {
		plan, err := s.planFor(ctx, q, p.WorkspaceID)
		if err != nil {
			return err
		}
		n, err := q.CountRules(ctx, p.WorkspaceID)
		if err != nil {
			return err
		}
		if err := checkCount(plan, LimitRules, plan.Limits.Rules, n); err != nil {
			return err
		}
		if err := q.CreateRule(ctx, db.CreateRuleParams{ID: r.ID, WorkspaceID: p.WorkspaceID, Name: r.Name, Priority: r.Priority,
			Enabled: b2i(r.Enabled), Match: mustJSON(r.Match), Action: r.Action, Params: mustJSON(r.Params), CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err = s.emit(ctx, q, p, "rule.created", "", "", map[string]any{"rule": r})
		return err
	})
	return r, err
}

func (s *Service) ListRules(ctx context.Context, p Principal) ([]Rule, error) {
	if err := p.require(ScopeRulesManage); err != nil {
		return nil, err
	}
	rows, err := s.Q.ListRules(ctx, p.WorkspaceID)
	out := []Rule{}
	for _, r := range rows {
		out = append(out, toRule(r))
	}
	return out, err
}

func (s *Service) GetRule(ctx context.Context, p Principal, id string) (Rule, error) {
	if err := p.require(ScopeRulesManage); err != nil {
		return Rule{}, err
	}
	r, err := s.Q.GetRule(ctx, db.GetRuleParams{WorkspaceID: p.WorkspaceID, ID: id})
	if isNoRows(err) {
		return Rule{}, notFound("rule")
	}
	return toRule(r), err
}

func (s *Service) UpdateRule(ctx context.Context, p Principal, id string, in RuleInput) (Rule, error) {
	r, err := s.GetRule(ctx, p, id)
	if err != nil {
		return Rule{}, err
	}
	applyRuleInput(&r, in)
	if err := validateRule(&r); err != nil {
		return Rule{}, err
	}
	r.UpdatedAt = s.now()
	err = s.tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateRule(ctx, db.UpdateRuleParams{Name: r.Name, Priority: r.Priority, Enabled: b2i(r.Enabled),
			Match: mustJSON(r.Match), Action: r.Action, Params: mustJSON(r.Params), UpdatedAt: r.UpdatedAt,
			WorkspaceID: p.WorkspaceID, ID: id}); err != nil {
			return err
		}
		_, err := s.emit(ctx, q, p, "rule.updated", "", "", map[string]any{"rule": r})
		return err
	})
	return r, err
}

func (s *Service) DeleteRule(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeRulesManage); err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		n, err := q.DeleteRule(ctx, db.DeleteRuleParams{WorkspaceID: p.WorkspaceID, ID: id})
		if err != nil {
			return err
		}
		if n == 0 {
			return notFound("rule")
		}
		_, err = s.emit(ctx, q, p, "rule.deleted", "", "", map[string]any{"rule_id": id})
		return err
	})
}

// ---- evaluation ----

func globMatch(pattern, s string) bool {
	re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(pattern)) + "$"
	ok, _ := regexp.MatchString(re, s)
	return ok
}

func metadataPath(meta map[string]any, path string) (any, bool) {
	var cur any = meta
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[part]; !ok {
			return nil, false
		}
	}
	return cur, cur != nil
}

func condHolds(c RuleCondition, meta map[string]any) bool {
	v, ok := metadataPath(meta, c.Path)
	if c.Op == "exists" {
		return ok
	}
	if !ok {
		return false
	}
	num := func(x any) (float64, bool) { f, ok := x.(float64); return f, ok }
	switch c.Op {
	case "eq":
		return fmt.Sprint(v) == fmt.Sprint(c.Value)
	case "ne":
		return fmt.Sprint(v) != fmt.Sprint(c.Value)
	case "lt", "lte", "gt", "gte":
		a, ok1 := num(v)
		b, ok2 := num(c.Value)
		if !ok1 || !ok2 {
			return false
		}
		return map[string]bool{"lt": a < b, "lte": a <= b, "gt": a > b, "gte": a >= b}[c.Op]
	case "contains":
		if s, ok := v.(string); ok {
			return strings.Contains(strings.ToLower(s), strings.ToLower(fmt.Sprint(c.Value)))
		}
		if arr, ok := v.([]any); ok {
			for _, x := range arr {
				if fmt.Sprint(x) == fmt.Sprint(c.Value) {
					return true
				}
			}
		}
		return false
	case "in", "not_in":
		list, _ := c.Value.([]any)
		found := false
		for _, x := range list {
			found = found || fmt.Sprint(x) == fmt.Sprint(v)
		}
		return found == (c.Op == "in")
	}
	return false
}

func ruleMatches(r Rule, it db.Item) bool {
	m := r.Match
	if len(m.CollectionIDs) > 0 && !slices.Contains(m.CollectionIDs, it.CollectionID) {
		return false
	}
	if len(m.Types) > 0 && !slices.Contains(m.Types, it.Type) {
		return false
	}
	if m.SourcePattern != "" && !globMatch(m.SourcePattern, it.Source) {
		return false
	}
	if m.TextContains != "" {
		needle := strings.ToLower(m.TextContains)
		if !strings.Contains(strings.ToLower(it.Title), needle) && !strings.Contains(strings.ToLower(it.Content), needle) {
			return false
		}
	}
	if len(m.Conditions) > 0 {
		var meta map[string]any
		_ = json.Unmarshal([]byte(it.Metadata), &meta)
		for _, c := range m.Conditions {
			if !condHolds(c, meta) {
				return false
			}
		}
	}
	return true
}

// applyRules runs inside CreateItem's transaction right after the item and
// its item.created event are stored. It returns the (possibly changed) item.
func (s *Service) applyRules(ctx context.Context, q *db.Queries, p Principal, it db.Item, col Collection) (db.Item, error) {
	if it.Status != "pending" {
		return it, nil
	}
	rows, err := q.ListEnabledRules(ctx, p.WorkspaceID)
	if err != nil || len(rows) == 0 {
		return it, err
	}
	for _, row := range rows {
		r := toRule(row)
		if !ruleMatches(r, it) {
			continue
		}
		if err := q.BumpRule(ctx, r.ID); err != nil {
			return it, err
		}
		actor := Principal{WorkspaceID: p.WorkspaceID, Kind: PrincipalSystem, ID: r.ID, Name: "Rule: " + r.Name}
		ruleInfo := map[string]any{"id": r.ID, "name": r.Name, "action": r.Action}
		switch r.Action {
		case RuleEscalate:
			_, err = s.emit(ctx, q, actor, "item.escalated", it.CollectionID, it.ID,
				s.itemEventData(ctx, it, col, map[string]any{"rule": ruleInfo, "note": r.Params.Note, "channel_id": r.Params.ChannelID}))
			return it, err
		case RuleExpire:
			exp := formatTime(s.Now().Add(time.Duration(r.Params.AfterSeconds) * time.Second))
			if err := q.SetItemExpiry(ctx, db.SetItemExpiryParams{ExpiresAt: nullStr(exp), ID: it.ID}); err != nil {
				return it, err
			}
			_, err = s.emit(ctx, q, actor, "item.rule_applied", it.CollectionID, it.ID,
				s.itemEventData(ctx, it, col, map[string]any{"rule": ruleInfo, "expires_at": exp}))
			return it, err
		}
		// Status-changing actions go through the normal mutation path (version
		// snapshot + event), so webhooks see e.g. a regular item.approved.
		return s.mutateItem(ctx, q, actor, it.ID, nil, func(cur *db.Item, _ Collection) (string, map[string]any, error) {
			extra := map[string]any{"rule": ruleInfo, "feedback": r.Params.Feedback}
			switch r.Action {
			case RuleAutoApprove:
				cur.Status, cur.DecidedAt, cur.Feedback = "approved", nullStr(s.now()), r.Params.Feedback
				return "item.approved", extra, nil
			case RuleAutoReject:
				cur.Status, cur.DecidedAt, cur.Feedback = "rejected", nullStr(s.now()), r.Params.Feedback
				return "item.rejected", extra, nil
			default: // set_status
				cur.Status = r.Params.Status
				return "item.status_changed", extra, nil
			}
		})
	}
	return it, nil
}

// expireItems moves pending items past their expires_at (set by expires_in
// on creation or by an "expire" rule) to the "expired" status.
func (s *Service) expireItems(ctx context.Context) {
	due, err := s.Q.ExpiredItems(ctx, nullStr(s.now()))
	if err != nil {
		return
	}
	for _, d := range due {
		actor := Principal{WorkspaceID: d.WorkspaceID, Kind: PrincipalSystem, ID: "expiry", Name: "Expiry"}
		err := s.tx(ctx, func(q *db.Queries) error {
			if err := q.ClearItemExpiry(ctx, d.ID); err != nil {
				return err
			}
			_, err := s.mutateItem(ctx, q, actor, d.ID, nil, func(cur *db.Item, _ Collection) (string, map[string]any, error) {
				if cur.Status != "pending" {
					return "", nil, conflict("no longer pending")
				}
				cur.Status, cur.DecidedAt, cur.Feedback = "expired", nullStr(s.now()), "Expired before review"
				return "item.expired", map[string]any{"feedback": cur.Feedback}, nil
			})
			return err
		})
		if err != nil && !isConflict(err) {
			s.Log.Warn("expire item", "item_id", d.ID, "err", err)
		}
	}
}
