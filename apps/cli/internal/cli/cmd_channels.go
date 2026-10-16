package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type NotificationChannel struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	Type                string         `json:"type"`
	Enabled             bool           `json:"enabled"`
	Config              map[string]any `json:"config"`
	Events              []string       `json:"events"`
	CollectionIDs       []string       `json:"collection_ids"`
	DigestWindowSeconds int            `json:"digest_window_seconds"`
	FailCount           int            `json:"fail_count"`
	LastError           string         `json:"last_error"`
	DisabledReason      string         `json:"disabled_reason"`
	LastSentAt          *string        `json:"last_sent_at"`
	Interactive         bool           `json:"interactive"`
	IntegrationURL      string         `json:"integration_url"`
	CreatedAt           string         `json:"created_at"`
}

type NotificationDelivery struct {
	ID        string  `json:"id"`
	EventType string  `json:"event_type"`
	ItemID    *string `json:"item_id"`
	Status    string  `json:"status"`
	Attempts  int     `json:"attempts"`
	LastError string  `json:"last_error"`
	SentAt    *string `json:"sent_at"`
	CreatedAt string  `json:"created_at"`
}

func channelsCommand() *Command {
	return &Command{
		Name: "channels", Aliases: []string{"channel", "notification-channels", "notifications"},
		Short: "Manage notification channels (Telegram, Slack, Discord, ntfy, email)",
		Subs: []*Command{
			leaf("list", "", "List notification channels", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []NotificationChannel }
					raw, err := cl.Get(c, "/v1/notification-channels", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "TYPE", "ENABLED", "EVENTS", "FAILS", "LAST SENT", "ERROR")
					for _, ch := range page.Data {
						t.row(ch.ID, truncate(ch.Name, 30), ch.Type, yesNo(ch.Enabled), strings.Join(ch.Events, ","),
							strconv.Itoa(ch.FailCount), fmtTime(strPtr(ch.LastSentAt)), truncate(orDash(ch.LastError), 40))
					}
					t.flush("No notification channels. Create one with: staqly channels create --type ntfy --name Phone --config topic=<topic>")
					return nil
				}
			}, "staqly channels list"),

			leaf("get", "<id>", "Show a channel and its settings (secrets masked)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var ch NotificationChannel
					raw, err := cl.Get(c, "/v1/notification-channels/"+url.PathEscape(args[0]), nil, &ch)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					printChannel(c, ch)
					return nil
				}
			}, "staqly channels get nch_oalirbz67y4m4cqxq5dw"),

			channelWriteCommand(true),
			channelWriteCommand(false),

			leaf("delete", "<id>", "Delete a notification channel", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/notification-channels/"+url.PathEscape(args[0]), "channel", args[0])
				}
			}, "staqly channels delete nch_oalirbz67y4m4cqxq5dw"),

			leaf("test", "<id>", "Send a test message now (exit 1 if it fails)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var res struct {
						OK    bool   `json:"ok"`
						Error string `json:"error"`
					}
					raw, err := cl.Post(c, "/v1/notification-channels/"+url.PathEscape(args[0])+"/test", nil, &res)
					if err != nil {
						return err
					}
					if c.JSON {
						if err := c.printRaw(raw); err != nil {
							return err
						}
						if !res.OK {
							return exitWith(ExitError)
						}
						return nil
					}
					if !res.OK {
						return &exitErr{code: ExitError, msg: "test message failed: " + res.Error}
					}
					fmt.Fprintf(c.Out, "Sent a test message to %s\n", args[0])
					return nil
				}
			}, "staqly channels test nch_oalirbz67y4m4cqxq5dw"),

			leaf("deliveries", "<id>", "Show a channel's recent notifications", 1, 1, func(fs *flag.FlagSet) runFunc {
				limit := fs.Int("limit", 50, "how many (max 200)")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []NotificationDelivery }
					raw, err := cl.Get(c, "/v1/notification-channels/"+url.PathEscape(args[0])+"/deliveries",
						url.Values{"limit": {strconv.Itoa(*limit)}}, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "EVENT", "ITEM", "STATUS", "ATTEMPTS", "CREATED", "SENT", "ERROR")
					for _, d := range page.Data {
						t.row(d.ID, d.EventType, orDash(strPtr(d.ItemID)), c.status(d.Status), strconv.Itoa(d.Attempts),
							fmtTime(d.CreatedAt), fmtTime(strPtr(d.SentAt)), truncate(orDash(d.LastError), 40))
					}
					t.flush("No notifications yet.")
					return nil
				}
			}, "staqly channels deliveries nch_oalirbz67y4m4cqxq5dw --limit 20"),
		},
	}
}

// channelWriteCommand builds `channels create` (create=true) or `channels update`.
func channelWriteCommand(create bool) *Command {
	name, args, short, minArgs := "update", "<id>", "Update a channel (config keys merge; key=null removes one)", 1
	if create {
		name, args, short, minArgs = "create", "", "Create a notification channel", 0
	}
	cmd := leaf(name, args, short, minArgs, minArgs, func(fs *flag.FlagSet) runFunc {
		typ := new(string)
		if create {
			fs.StringVar(typ, "type", "", "telegram, slack, discord, ntfy or email (required)")
		}
		chName := fs.String("name", "", "channel name")
		var cfg multiFlag
		fs.Var(&cfg, "config", "setting `key=value` (repeatable), e.g. topic=staqly-alerts, chat_id=123, to=a@example.com,b@example.com")
		cfgJSON := fs.String("config-json", "", "settings as a JSON object; '-' reads stdin, '@path' reads a file (keeps secrets out of shell history)")
		events := &multiFlag{comma: true}
		fs.Var(events, "events", "event `patterns`, comma-separated or repeated (default item.created)")
		cols := &multiFlag{comma: true}
		fs.Var(cols, "collection", "limit to `collections` (ID or name; repeatable)")
		allCols := new(bool)
		if !create {
			fs.BoolVar(allCols, "all-collections", false, "notify for every collection again")
		}
		digest := fs.String("digest", "", "batch events within this `window` into one message, e.g. 5m (0 = send immediately)")
		disabled := new(bool)
		enable := new(bool)
		if create {
			fs.BoolVar(disabled, "disabled", false, "create disabled")
		} else {
			fs.BoolVar(enable, "enable", false, "enable the channel (also resets the failure counter)")
			fs.BoolVar(disabled, "disable", false, "disable the channel (revokes its one-tap decision links)")
		}
		return func(c *Ctx, a []string) error {
			body := map[string]any{}
			setIf(body, "name", *chName)
			if create {
				if *typ == "" || *chName == "" {
					return usageErrf("--type and --name are required")
				}
				body["type"] = *typ
			}
			if *enable && *disabled {
				return usageErrf("--enable and --disable cannot be combined")
			}
			if *enable {
				body["enabled"] = true
			}
			if *disabled {
				body["enabled"] = false
			}
			config := map[string]any{}
			if *cfgJSON != "" {
				m, err := readJSONObjectArg(c, "config-json", *cfgJSON)
				if err != nil {
					return err
				}
				config = m
			}
			for _, kv := range cfg.vals {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return usageErrf("--config expects key=value, got %q", kv)
				}
				config[k] = channelConfigValue(k, v)
			}
			if len(config) > 0 {
				body["config"] = config
			} else if create {
				return usageErrf("--config or --config-json is required (e.g. --config topic=<topic> for ntfy)")
			}
			if len(events.vals) > 0 {
				body["events"] = events.vals
			}
			if *digest != "" {
				secs, err := parseSeconds(*digest)
				if err != nil {
					return usageErrf("--digest must be a duration like 5m, or 0")
				}
				body["digest_window_seconds"] = secs
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			if *allCols {
				if len(cols.vals) > 0 {
					return usageErrf("--all-collections and --collection cannot be combined")
				}
				body["collection_ids"] = []string{}
			}
			if len(cols.vals) > 0 {
				ids := make([]string, len(cols.vals))
				for i, ref := range cols.vals {
					if ids[i], err = resolveCollectionID(c, cl, ref); err != nil {
						return err
					}
				}
				body["collection_ids"] = ids
			}
			var ch NotificationChannel
			var raw json.RawMessage
			if create {
				raw, err = cl.Post(c, "/v1/notification-channels", body, &ch)
			} else {
				if len(body) == 0 {
					return usageErrf("nothing to update; pass at least one of --name, --config, --config-json, --events, --collection, --all-collections, --digest, --enable, --disable")
				}
				raw, err = cl.Patch(c, "/v1/notification-channels/"+url.PathEscape(a[0]), body, &ch)
			}
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			verb := "Updated"
			if create {
				verb = "Created"
			}
			fmt.Fprintf(c.Out, "%s %s channel %s (%s)\n", verb, ch.Type, c.paint(bold, ch.Name), ch.ID)
			if ch.IntegrationURL != "" {
				fmt.Fprintf(c.Out, "Integration URL: %s\n", ch.IntegrationURL)
			}
			if create {
				fmt.Fprintf(c.Out, "Send a test message with: staqly channels test %s\n", ch.ID)
			}
			return nil
		}
	})
	if create {
		cmd.Examples = []string{
			"staqly channels create --type ntfy --name Phone --config topic=staqly-alerts-7f3k",
			"staqly channels create --type telegram --name Team --config chat_id=-1001234567890 --config-json @telegram.json",
			"echo '{\"webhook_url\":\"https://hooks.slack.com/services/T000/B000/XXXX\"}' | staqly channels create --type slack --name Reviews --config-json -",
			"staqly channels create --type email --name Ops --config host=smtp.example.com --config port=587 --config from=staqly@example.com --config to=ops@example.com --events 'item.*' --digest 10m",
		}
		cmd.Long = `Create a notification channel. Settings depend on the type (* = required):

  telegram  bot_token*, chat_id*, mode (polling|webhook), approver_user_ids
  slack     webhook_url*, or bot_token* + channel*; signing_secret enables buttons
  discord   webhook_url*, or bot_token* + channel*; application_id + public_key enable buttons
  ntfy      topic*, server_url (default https://ntfy.sh), access_token, priority (1-5)
  email     host*, from*, to*, port, username, password, tls (starttls|tls|none)

Secrets (bot_token, webhook_url, signing_secret, access_token, password) are
stored encrypted and always shown masked. Pass them with --config-json - or
--config-json @file to keep them out of your shell history.`
	} else {
		cmd.Examples = []string{
			"staqly channels update nch_oalirbz67y4m4cqxq5dw --disable",
			"staqly channels update nch_oalirbz67y4m4cqxq5dw --events item.created,item.approved --collection \"Blog posts\"",
			"staqly channels update nch_oalirbz67y4m4cqxq5dw --config priority=5 --config access_token=null",
		}
	}
	return cmd
}

// channelConfigValue types a --config value: numbers for port/priority,
// lists for to/approver_user_ids, null removes the key, else a string.
func channelConfigValue(k, v string) any {
	switch {
	case v == "null":
		return nil
	case k == "port" || k == "priority":
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	case k == "to" || k == "approver_user_ids":
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			return scalarOrJSON(v)
		}
		out := []string{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return v
}

// parseSeconds accepts a Go duration (5m) or a whole number of seconds.
func parseSeconds(v string) (int, error) {
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", v)
	}
	return int(d.Round(time.Second).Seconds()), nil
}

// readJSONObjectArg parses a JSON object given inline, as '-' (stdin) or as '@path'.
func readJSONObjectArg(c *Ctx, flagName, v string) (map[string]any, error) {
	switch {
	case v == "-":
		b, err := io.ReadAll(io.LimitReader(c.Env.Stdin, 1<<20))
		if err != nil {
			return nil, err
		}
		v = string(b)
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		if err != nil {
			return nil, fmt.Errorf("read --%s: %w", flagName, err)
		}
		v = string(b)
	}
	return parseJSONObject(flagName, v)
}

func printChannel(c *Ctx, ch NotificationChannel) {
	cols := "all"
	if len(ch.CollectionIDs) > 0 {
		cols = strings.Join(ch.CollectionIDs, ", ")
	}
	digest := "immediately"
	if ch.DigestWindowSeconds > 0 {
		digest = (time.Duration(ch.DigestWindowSeconds) * time.Second).String()
	}
	c.kv("ID", ch.ID, "Name", ch.Name, "Type", ch.Type, "Enabled", yesNo(ch.Enabled), "Disabled because", ch.DisabledReason,
		"Events", strings.Join(ch.Events, ", "), "Collections", cols, "Digest", digest,
		"Buttons", map[bool]string{true: "approve/reject in chat", false: "one-tap links"}[ch.Interactive],
		"Failures", strconv.Itoa(ch.FailCount), "Last error", ch.LastError, "Last sent", fmtOptTime(ch.LastSentAt),
		"Integration URL", ch.IntegrationURL, "Created", fmtTime(ch.CreatedAt))
	if len(ch.Config) > 0 {
		fmt.Fprintln(c.Out)
		keys := make([]string, 0, len(ch.Config))
		for k := range ch.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t := c.table("SETTING", "VALUE")
		for _, k := range keys {
			v := ch.Config[k]
			s, ok := v.(string)
			if !ok {
				b, _ := json.Marshal(v)
				s = string(b)
			}
			t.row(k, s)
		}
		t.flush("")
	}
}
