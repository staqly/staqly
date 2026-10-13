package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var ruleOps = map[string]bool{"eq": true, "ne": true, "lt": true, "lte": true, "gt": true, "gte": true,
	"contains": true, "in": true, "not_in": true, "exists": true}

func rulesCommand() *Command {
	return &Command{
		Name: "rules", Aliases: []string{"rule"},
		Short: "Manage approval rules (auto-approve, auto-reject, expire, escalate)",
		Subs: []*Command{
			leaf("list", "", "List rules in evaluation order", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []Rule }
					raw, err := cl.Get(c, "/v1/rules", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "PRIORITY", "NAME", "ACTION", "ENABLED", "APPLIED", "MATCH")
					for _, r := range page.Data {
						t.row(r.ID, strconv.Itoa(r.Priority), truncate(r.Name, 30), r.Action, yesNo(r.Enabled),
							strconv.Itoa(r.TimesApplied), truncate(compactJSON(r.Match), 60))
					}
					t.flush("No rules.")
					return nil
				}
			}, "staqly rules list"),

			rulesCreateCommand(),

			leaf("delete", "<id>", "Delete a rule", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/rules/"+url.PathEscape(args[0]), "rule", args[0])
				}
			}, "staqly rules delete rul_123"),
		},
	}
}

func rulesCreateCommand() *Command {
	cmd := leaf("create", "", "Create a rule from flags or from a JSON file", 0, 0, func(fs *flag.FlagSet) runFunc {
		file := fs.String("file", "", "read the rule (RuleInput JSON) from this file; '-' = stdin")
		name := fs.String("name", "", "rule name")
		action := fs.String("action", "", "auto_approve, auto_reject, set_status, expire or escalate")
		priority := fs.Int("priority", 0, "lower runs first")
		cols := &multiFlag{comma: true}
		fs.Var(cols, "collection", "match `collections` (ID or name; repeatable)")
		types := &multiFlag{comma: true}
		fs.Var(types, "type", "match item `types` (repeatable)")
		source := fs.String("source", "", "glob on item.source (* and ?)")
		contains := fs.String("contains", "", "case-insensitive text in title or content")
		var when multiFlag
		fs.Var(&when, "when", "metadata `condition` 'path op value', op: eq ne lt lte gt gte contains in not_in exists (repeatable)")
		feedback := fs.String("feedback", "", "params.feedback (for auto_reject / auto_approve)")
		setStatus := fs.String("set-status", "", "params.status (for set_status)")
		after := fs.Duration("after", 0, "params.after_seconds (for expire / escalate), e.g. 24h")
		note := fs.String("note", "", "params.note")
		disabled := fs.Bool("disabled", false, "create disabled")
		return func(c *Ctx, _ []string) error {
			var body map[string]any
			if *file != "" {
				others := 0
				fs.Visit(func(f *flag.Flag) {
					if f.Name != "file" && !globalNames[f.Name] && f.Name != "url" {
						others++
					}
				})
				if others > 0 {
					return usageErrf("--file cannot be combined with rule flags")
				}
				var data []byte
				var err error
				if *file == "-" {
					data, err = io.ReadAll(c.Env.Stdin)
				} else {
					data, err = os.ReadFile(*file)
				}
				if err != nil {
					return fmt.Errorf("read --file: %w", err)
				}
				if err := json.Unmarshal(data, &body); err != nil || body == nil {
					return usageErrf("--file must contain a JSON object (RuleInput)")
				}
			} else {
				if *name == "" || *action == "" {
					return usageErrf("--name and --action are required (or pass --file rule.json)")
				}
				body = map[string]any{"name": *name, "action": *action, "enabled": !*disabled}
				if *priority != 0 {
					body["priority"] = *priority
				}
				match := map[string]any{}
				if len(types.vals) > 0 {
					match["types"] = types.vals
				}
				setIf(match, "source_pattern", *source)
				setIf(match, "text_contains", *contains)
				var conds []map[string]any
				for _, w := range when.vals {
					cond, err := parseCondition(w)
					if err != nil {
						return err
					}
					conds = append(conds, cond)
				}
				if len(conds) > 0 {
					match["conditions"] = conds
				}
				params := map[string]any{}
				setIf(params, "feedback", *feedback)
				setIf(params, "status", *setStatus)
				setIf(params, "note", *note)
				if *after > 0 {
					params["after_seconds"] = int(after.Round(time.Second).Seconds())
				}
				body["match"] = match
				body["params"] = params
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if len(cols.vals) > 0 {
				ids := make([]string, len(cols.vals))
				for i, ref := range cols.vals {
					if ids[i], err = resolveCollectionID(c, cl, ref); err != nil {
						return err
					}
				}
				body["match"].(map[string]any)["collection_ids"] = ids
			}
			var r Rule
			raw, err := cl.Post(c, "/v1/rules", body, &r)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Created rule %s (%s, priority %d)\n", r.ID, r.Action, r.Priority)
			return nil
		}
	},
		"staqly rules create --name \"Auto-approve tests\" --action auto_approve --collection Deploys --when 'env eq staging'",
		"staqly rules create --name \"Expire stale\" --action expire --after 72h",
		"staqly rules create --name \"Big spend\" --action escalate --when 'amount gt 1000' --after 1h --note \"ping finance\"",
		"staqly rules create --file rule.json")
	cmd.Long = `Create an approval rule. Rules run on new pending items, lowest priority
first; the first match wins. Build the rule from flags, or pass the full
RuleInput JSON with --file (see apps/api/openapi.yaml).`
	return cmd
}

// parseCondition parses "path op value" (value is JSON when it parses, e.g.
// numbers or ["a","b"]; for in/not_in a comma list is also accepted).
func parseCondition(s string) (map[string]any, error) {
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return nil, usageErrf("--when expects 'path op value', got %q", s)
	}
	path, op := parts[0], parts[1]
	if !ruleOps[op] {
		return nil, usageErrf("--when: unknown operator %q", op)
	}
	cond := map[string]any{"path": path, "op": op}
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), path))
	raw = strings.TrimSpace(strings.TrimPrefix(raw, op))
	switch {
	case op == "exists":
		if raw != "" {
			cond["value"] = scalarOrJSON(raw)
		}
	case raw == "":
		return nil, usageErrf("--when %q needs a value", s)
	case (op == "in" || op == "not_in") && !strings.HasPrefix(raw, "["):
		var vals []any
		for _, v := range strings.Split(raw, ",") {
			vals = append(vals, scalarOrJSON(strings.TrimSpace(v)))
		}
		cond["value"] = vals
	default:
		cond["value"] = scalarOrJSON(raw)
	}
	return cond, nil
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
