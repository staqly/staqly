package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

func channelsCommand() *Command {
	return &Command{
		Name: "channels", Aliases: []string{"channel"},
		Short: "Connect and manage the accounts posts go to",
		Subs: []*Command{
			channelsProvidersCommand(),
			leaf("list", "", "List connected channels with their group and last post", 0, 0, func(fs *flag.FlagSet) runFunc {
				group := fs.String("group", "", "only channels in this channel `group` (name or ID)")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					groups, err := channelGroups(c, cl)
					if err != nil {
						return err
					}
					q := url.Values{}
					if *group != "" {
						id, err := resolveGroupID(groups, *group)
						if err != nil {
							return err
						}
						q.Set("group", id)
					}
					var page struct{ Data []Channel }
					raw, err := cl.Get(c, "/v1/channels", q, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					names := map[string]string{}
					for _, g := range groups {
						names[g.ID] = g.Name
					}
					t := c.table("ID", "NAME", "PROVIDER", "HANDLE", "STATUS", "GROUP", "LAST POST")
					for _, ch := range page.Data {
						last, _ := ch.lastAndNext()
						t.row(ch.ID, truncate(ch.Name, 30), ch.Provider, truncate(orDash(ch.Handle), 30), c.status(ch.Status),
							truncate(orDash(names[ch.GroupID]), 24), last)
					}
					t.flush("No channels yet. See the platforms with: staqly channels providers")
					return nil
				}
			}, "staqly channels list", "staqly channels list --group \"Client A\""),
			leaf("get", "<id|name>", "Show a channel (credentials are never returned)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					id, err := resolveChannelID(c, cl, args[0])
					if err != nil {
						return err
					}
					var ch Channel
					raw, err := cl.Get(c, "/v1/channels/"+url.PathEscape(id), nil, &ch)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					printChannel(c, ch)
					return nil
				}
			}, "staqly channels get ch_oalirbz67y4m4cqxq5dw"),
			channelsConnectCommand(),
			channelsUpdateCommand(),
			leaf("remove", "<id|name>", "Remove a channel; targets on it go with it", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					id, err := resolveChannelID(c, cl, args[0])
					if err != nil {
						return err
					}
					return simpleDelete(c, "/v1/channels/"+url.PathEscape(id), "channel", id)
				}
			}, "staqly channels remove ch_oalirbz67y4m4cqxq5dw"),
			leaf("test", "<id|name>", "Check the stored credentials against the platform (exit 1 if they fail)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					id, err := resolveChannelID(c, cl, args[0])
					if err != nil {
						return err
					}
					var res struct {
						OK      bool           `json:"ok"`
						Error   string         `json:"error"`
						Profile map[string]any `json:"profile"`
					}
					raw, err := cl.Post(c, "/v1/channels/"+url.PathEscape(id)+"/test", nil, &res)
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
						return &exitErr{code: ExitError, msg: "the platform refused the credentials: " + res.Error}
					}
					fmt.Fprintf(c.Out, "Connected as %s\n", firstNonEmpty(fmt.Sprint(res.Profile["handle"]), fmt.Sprint(res.Profile["name"]), id))
					return nil
				}
			}, "staqly channels test ch_oalirbz67y4m4cqxq5dw"),
			channelsAppCommand(),
			channelGroupsCommand(),
			channelLinksCommand(),
		},
	}
}

func channelsProvidersCommand() *Command {
	return leaf("providers", "[id]", "The platforms staqly can publish to, or one platform's fields and limits", 0, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			var page struct{ Data []Provider }
			raw, err := cl.Get(c, "/v1/providers", nil, &page)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				for _, p := range page.Data {
					if p.ID == args[0] {
						if c.JSON {
							return c.printJSON(p)
						}
						printProvider(c, p)
						return nil
					}
				}
				return &exitErr{code: ExitError, msg: fmt.Sprintf("no provider %q; see staqly channels providers", args[0])}
			}
			if c.JSON {
				return c.printRaw(raw)
			}
			t := c.table("ID", "NAME", "AUTH", "STATUS", "APP", "MAX CHARS", "MEDIA")
			for _, p := range page.Data {
				app := "-"
				if p.Auth == "oauth2" {
					app = map[bool]string{true: "configured", false: "missing"}[p.AppConfigured]
				}
				t.row(p.ID, p.Name, p.Auth, c.status(p.Status), app, zeroDash(p.Limits.MaxChars, "no limit"), zeroDash(p.Limits.MaxMedia, "none"))
			}
			t.flush("No providers.")
			return nil
		}
	}, "staqly channels providers", "staqly channels providers telegram")
}

func zeroDash(n int, zero string) string {
	if n == 0 {
		return zero
	}
	return strconv.Itoa(n)
}

func printProvider(c *Ctx, p Provider) {
	c.kv("ID", p.ID, "Name", p.Name, "Auth", p.Auth, "Status", c.status(p.Status),
		"Developer app", map[bool]string{true: "configured", false: "missing"}[p.AppConfigured],
		"Max characters", zeroDash(p.Limits.MaxChars, "no limit"), "Max media", zeroDash(p.Limits.MaxMedia, "none"),
		"Media kinds", strings.Join(p.Limits.MediaKinds, ", "), "Needs title", yesNo(p.Limits.NeedsTitle),
		"Needs media", yesNo(p.Limits.NeedsMedia), "Needs link", yesNo(p.Limits.NeedsLink), "Docs", p.DocsURL)
	printFields(c, "CREDENTIAL", p.CredentialFields)
	printFields(c, "SETTING", p.SettingsFields)
}

func printFields(c *Ctx, what string, fields []ProviderField) {
	if len(fields) == 0 {
		return
	}
	fmt.Fprintln(c.Out)
	t := c.table(what, "LABEL", "KIND", "REQUIRED", "DEFAULT", "OPTIONS")
	for _, f := range fields {
		opts := make([]string, len(f.Options))
		for i, o := range f.Options {
			opts[i] = o.Value
		}
		t.row(f.Key, f.Label, f.Kind, yesNo(f.Required), orDash(f.Default), truncate(orDash(strings.Join(opts, ", ")), 40))
	}
	t.flush("")
}

func printChannel(c *Ctx, ch Channel) {
	last, next := ch.lastAndNext()
	recent := ""
	if ch.Activity != nil {
		recent = fmt.Sprint(ch.Activity.Published30d)
	}
	c.kv("ID", ch.ID, "Name", ch.Name, "Provider", ch.Provider, "Handle", ch.Handle, "Profile", ch.ProfileURL,
		"Status", c.status(ch.Status), "Group", ch.GroupID, "Last post", last, "Next post", next,
		"Posts, 30 days", recent, "Last error", ch.LastError, "Created", fmtTime(ch.CreatedAt))
	if len(ch.Settings) > 0 {
		fmt.Fprintln(c.Out)
		keys := make([]string, 0, len(ch.Settings))
		for k := range ch.Settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t := c.table("SETTING", "VALUE")
		for _, k := range keys {
			t.row(k, jsonScalar(ch.Settings[k]))
		}
		t.flush("")
	}
}

func jsonScalar(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// kvFlags reads repeated key=value flags into a map; a value of null removes the key.
func kvFlags(flagName string, vals []string, typed bool) (map[string]any, error) {
	out := map[string]any{}
	for _, kv := range vals {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, usageErrf("--%s expects key=value, got %q", flagName, kv)
		}
		switch {
		case v == "null":
			out[k] = nil
		case typed:
			out[k] = scalarOrJSON(v)
		default:
			out[k] = v
		}
	}
	return out, nil
}

func channelsConnectCommand() *Command {
	cmd := leaf("connect", "<provider>", "Connect an account: token platforms and your own automation here, OAuth platforms through a link", 1, 1, func(fs *flag.FlagSet) runFunc {
		name := fs.String("name", "", "what to call the channel (default: the account's name)")
		var fields multiFlag
		fs.Var(&fields, "field", "credential `key=value` from `channels providers <id>` (repeatable)")
		fieldsJSON := fs.String("fields-json", "", "credentials as a JSON object; '-' reads stdin, '@path' reads a file (keeps secrets out of shell history)")
		var settings multiFlag
		fs.Var(&settings, "setting", "default target `key=value` for new posts (repeatable)")
		instance := fs.String("instance", "", "platforms with many servers (Mastodon): the server's URL or host")
		group := fs.String("group", "", "put the channel in this channel `group` (name or ID; default: Default)")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			provider := args[0]
			var page struct{ Data []Provider }
			if _, err := cl.Get(c, "/v1/providers", nil, &page); err != nil {
				return err
			}
			var prov *Provider
			for i := range page.Data {
				if page.Data[i].ID == provider {
					prov = &page.Data[i]
				}
			}
			if prov == nil {
				return &exitErr{code: ExitError, msg: fmt.Sprintf("no provider %q; see staqly channels providers", provider)}
			}
			if prov.Status != "available" {
				return &exitErr{code: ExitError, msg: fmt.Sprintf("%s is not available yet", prov.Name)}
			}
			if prov.Auth == "oauth2" {
				if !prov.AppConfigured {
					return &exitErr{code: ExitError, msg: fmt.Sprintf("%s needs a developer app first: staqly channels app %s --client-id ... --client-secret ...", prov.Name, provider)}
				}
				q := url.Values{}
				setQ(q, "instance", *instance)
				var res struct {
					URL string `json:"url"`
				}
				raw, err := cl.Get(c, "/v1/channels/oauth/"+url.PathEscape(provider)+"/start", q, &res)
				if err != nil || c.JSON {
					return jsonOr(c, raw, err)
				}
				fmt.Fprintf(c.Stderr, "Open this link in a browser signed in to %s; the channel appears in the dashboard when you are done:\n", prov.Name)
				fmt.Fprintln(c.Out, res.URL)
				return nil
			}
			body := map[string]any{"provider": provider}
			setIf(body, "name", *name)
			if *group != "" {
				groups, err := channelGroups(c, cl)
				if err != nil {
					return err
				}
				if body["group_id"], err = resolveGroupID(groups, *group); err != nil {
					return err
				}
			}
			creds := map[string]any{}
			if *fieldsJSON != "" {
				if creds, err = readJSONObjectArg(c, "fields-json", *fieldsJSON); err != nil {
					return err
				}
			}
			kv, err := kvFlags("field", fields.vals, false)
			if err != nil {
				return err
			}
			for k, v := range kv {
				creds[k] = v
			}
			for _, f := range prov.CredentialFields {
				applies := f.When == nil || fmt.Sprint(creds[f.When.Key]) == f.When.Value
				if f.Required && applies && creds[f.Key] == nil {
					return usageErrf("%s needs --field %s=... (%s)", prov.Name, f.Key, f.Label)
				}
			}
			if len(creds) > 0 {
				body["fields"] = creds
			}
			if len(settings.vals) > 0 {
				if body["settings"], err = kvFlags("setting", settings.vals, true); err != nil {
					return err
				}
			}
			var ch Channel
			raw, err := cl.Post(c, "/v1/channels", body, &ch)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Connected %s %s as %s (%s)\n", ch.Provider, c.paint(bold, ch.Name), orDash(ch.Handle), ch.ID)
			return nil
		}
	}, "staqly channels connect custom --name \"My pipeline\"",
		"staqly channels connect telegram --name Announcements --field chat_id=@mychannel --fields-json @telegram.json",
		"echo '{\"bot_token\":\"123:abc\"}' | staqly channels connect telegram --field chat_id=-100123 --fields-json -",
		"staqly channels connect mastodon --instance mastodon.social   # prints the sign-in link")
	cmd.Long = `Connect a channel. 'staqly channels providers <id>' lists the credential fields a
token platform needs (pass them with --field or --fields-json so secrets stay out
of your shell history); the platform is asked for the account's profile before the
channel is saved and the credentials are stored sealed. Provider custom is your own
automation and takes no fields: its posts wait for 'staqly posts result'.

OAuth platforms print a sign-in link to open in a browser instead (the server
needs a developer app: 'staqly channels app').`
	return cmd
}

func channelsUpdateCommand() *Command {
	cmd := leaf("update", "<id|name>", "Rename a channel, change its defaults, turn it off or on, or reconnect it", 1, 1, func(fs *flag.FlagSet) runFunc {
		name := fs.String("name", "", "new name")
		var settings multiFlag
		fs.Var(&settings, "setting", "default target `key=value` (repeatable; key=null removes one)")
		var fields multiFlag
		fs.Var(&fields, "field", "fresh credential `key=value` to reconnect a token platform (repeatable)")
		fieldsJSON := fs.String("fields-json", "", "fresh credentials as a JSON object; '-' reads stdin, '@path' reads a file")
		enable := fs.Bool("enable", false, "turn the channel on")
		disable := fs.Bool("disable", false, "turn the channel off: targets on it fail without trying")
		links := fs.String("links", "", "what the channel does with links in posts: `keep`, remove or defang (example[.]com); X allows remove and defang")
		group := fs.String("group", "", "move the channel to this channel `group` (name or ID)")
		return func(c *Ctx, args []string) error {
			if *enable && *disable {
				return usageErrf("--enable and --disable cannot be combined")
			}
			cl, err := c.Client()
			if err != nil {
				return err
			}
			id, err := resolveChannelID(c, cl, args[0])
			if err != nil {
				return err
			}
			body := map[string]any{}
			setIf(body, "name", *name)
			if *group != "" {
				groups, err := channelGroups(c, cl)
				if err != nil {
					return err
				}
				if body["group_id"], err = resolveGroupID(groups, *group); err != nil {
					return err
				}
			}
			if *enable {
				body["status"] = "connected"
			}
			if *disable {
				body["status"] = "disabled"
			}
			if len(settings.vals) > 0 {
				if body["settings"], err = kvFlags("setting", settings.vals, true); err != nil {
					return err
				}
			}
			if *links != "" {
				// settings replaces the channel's settings: start from the current ones so --links keeps the rest.
				var cur Channel
				if _, err := cl.Get(c, "/v1/channels/"+url.PathEscape(id), nil, &cur); err != nil {
					return err
				}
				merged := map[string]any{}
				for k, v := range cur.Settings {
					merged[k] = v
				}
				if extra, ok := body["settings"].(map[string]any); ok {
					for k, v := range extra {
						merged[k] = v
					}
				}
				merged["links"] = *links
				body["settings"] = merged
			}
			creds := map[string]any{}
			if *fieldsJSON != "" {
				if creds, err = readJSONObjectArg(c, "fields-json", *fieldsJSON); err != nil {
					return err
				}
			}
			kv, err := kvFlags("field", fields.vals, false)
			if err != nil {
				return err
			}
			for k, v := range kv {
				creds[k] = v
			}
			if len(creds) > 0 {
				body["fields"] = creds
			}
			if len(body) == 0 {
				return usageErrf("nothing to update; pass at least one of --name, --group, --setting, --links, --field, --fields-json, --enable, --disable")
			}
			var ch Channel
			raw, err := cl.Patch(c, "/v1/channels/"+url.PathEscape(id), body, &ch)
			if err != nil || c.JSON {
				return jsonOr(c, raw, err)
			}
			fmt.Fprintf(c.Out, "Updated %s (%s): %s\n", c.paint(bold, ch.Name), ch.ID, c.status(ch.Status))
			return nil
		}
	}, "staqly channels update ch_oalirbz67y4m4cqxq5dw --name \"Brand on Mastodon\"",
		"staqly channels update Mastodon --setting visibility=unlisted",
		"staqly channels update ch_oalirbz67y4m4cqxq5dw --disable",
		"staqly channels update X --links defang   # links go out as example[.]com",
		"staqly channels update X --group \"Client A\"",
		"staqly channels update Telegram --fields-json @telegram.json   # reconnect with a fresh token")
	return cmd
}

func channelsAppCommand() *Command {
	cmd := leaf("app", "<provider>", "Register (or --remove) the workspace's developer app for an OAuth platform", 1, 1, func(fs *flag.FlagSet) runFunc {
		clientID := fs.String("client-id", "", "the app's client id")
		secret := fs.String("client-secret", "", "the app's client secret; '-' reads stdin")
		remove := fs.Bool("remove", false, "remove the workspace's app (the server's PROVIDER_<ID>_CLIENT_ID applies again, if set)")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			path := "/v1/providers/" + url.PathEscape(args[0]) + "/app"
			if *remove {
				if err := cl.Delete(c, path, nil); err != nil {
					return err
				}
				if c.JSON {
					return c.printJSON(map[string]any{"removed": true, "provider": args[0]})
				}
				fmt.Fprintf(c.Out, "Removed the developer app for %s\n", args[0])
				return nil
			}
			if *clientID == "" || *secret == "" {
				return usageErrf("--client-id and --client-secret are required (or --remove)")
			}
			s := *secret
			if s == "-" {
				if s, err = readContent(c, "-", true); err != nil {
					return err
				}
				s = strings.TrimSpace(s)
			}
			if _, err := cl.call(c, Request{Method: "PUT", Path: path, Body: map[string]any{"client_id": *clientID, "client_secret": s}}, nil); err != nil {
				return err
			}
			if c.JSON {
				return c.printJSON(map[string]any{"saved": true, "provider": args[0]})
			}
			fmt.Fprintf(c.Out, "Saved the developer app for %s; connect with: staqly channels connect %s\n", args[0], args[0])
			return nil
		}
	}, "staqly channels app mastodon --client-id abc --client-secret -   # the secret from stdin", "staqly channels app mastodon --remove")
	cmd.Long = `OAuth platforms need a developer app (client id and secret) registered on the
platform with the redirect URL the dashboard shows. The server's
PROVIDER_<ID>_CLIENT_ID / _CLIENT_SECRET serve every workspace; this registers
the workspace's own instead.`
	return cmd
}

// channelGroups reads the workspace's channel groups (Default first).
func channelGroups(c *Ctx, cl *Client) ([]ChannelGroup, error) {
	var page struct{ Data []ChannelGroup }
	_, err := cl.Get(c, "/v1/channel-groups", nil, &page)
	return page.Data, err
}

// resolveGroupID takes a group ID or name (case-insensitive).
func resolveGroupID(groups []ChannelGroup, ref string) (string, error) {
	for _, g := range groups {
		if g.ID == ref || strings.EqualFold(g.Name, ref) {
			return g.ID, nil
		}
	}
	return "", &exitErr{code: ExitError, msg: fmt.Sprintf("no channel group %q; see staqly channels groups list", ref)}
}

func channelGroupsCommand() *Command {
	return &Command{
		Name: "groups", Aliases: []string{"group"},
		Short: "Group channels (one group per channel, e.g. one per client; Default always exists)",
		Subs: []*Command{
			leaf("list", "", "List channel groups with their channel counts", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []ChannelGroup }
					raw, err := cl.Get(c, "/v1/channel-groups", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "CHANNELS", "DEFAULT")
					for _, g := range page.Data {
						def := ""
						if g.Default {
							def = "yes"
						}
						t.row(g.ID, truncate(g.Name, 40), fmt.Sprint(g.Channels), orDash(def))
					}
					t.flush("")
					return nil
				}
			}, "staqly channels groups list"),
			leaf("create", "<name>", "Create a channel group", 1, 1, func(fs *flag.FlagSet) runFunc {
				color := fs.String("color", "", "a colour like `#22c55e`")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{"name": args[0]}
					setIf(body, "color", *color)
					var g ChannelGroup
					raw, err := cl.Post(c, "/v1/channel-groups", body, &g)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Created channel group %s (%s)\n", c.paint(bold, g.Name), g.ID)
					return nil
				}
			}, "staqly channels groups create \"Client A\""),
			leaf("rename", "<id|name> <new name>", "Rename a channel group", 2, 2, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					groups, err := channelGroups(c, cl)
					if err != nil {
						return err
					}
					id, err := resolveGroupID(groups, args[0])
					if err != nil {
						return err
					}
					var g ChannelGroup
					raw, err := cl.Patch(c, "/v1/channel-groups/"+url.PathEscape(id), map[string]any{"name": args[1]}, &g)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Renamed to %s (%s)\n", c.paint(bold, g.Name), g.ID)
					return nil
				}
			}, "staqly channels groups rename \"Client A\" \"Client Alpha\""),
			leaf("remove", "<id|name>", "Delete a channel group; its channels move to Default", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					groups, err := channelGroups(c, cl)
					if err != nil {
						return err
					}
					id, err := resolveGroupID(groups, args[0])
					if err != nil {
						return err
					}
					return simpleDelete(c, "/v1/channel-groups/"+url.PathEscape(id), "channel group", id)
				}
			}, "staqly channels groups remove \"Client A\""),
		},
	}
}
