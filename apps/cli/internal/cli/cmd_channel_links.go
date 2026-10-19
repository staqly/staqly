package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strings"
)

// ChannelLink is a connect link: someone without access opens it and connects a channel to the workspace.
type ChannelLink struct {
	ID        string   `json:"id"`
	Note      string   `json:"note"`
	Providers []string `json:"providers"`
	GroupID   string   `json:"group_id"`
	MaxUses   int      `json:"max_uses"`
	Uses      int      `json:"uses"`
	Status    string   `json:"status"`
	CreatedBy string   `json:"created_by"`
	ExpiresAt string   `json:"expires_at"`
	URL       string   `json:"url"`
}

func channelLinksCommand() *Command {
	return &Command{
		Name: "links", Aliases: []string{"link"},
		Short: "Connect links: let someone without access connect a channel to this workspace",
		Subs: []*Command{
			leaf("create", "", "Make a connect link (the link is printed once)", 0, 0, func(fs *flag.FlagSet) runFunc {
				note := fs.String("note", "", "shown to the person who opens the link")
				platforms := fs.String("platforms", "", "comma-separated platform ids to offer, e.g. `x,bluesky` (default: every available one)")
				group := fs.String("group", "", "the channel group connected channels join (`id|name`; default: Default)")
				hours := fs.Int("hours", 0, "how long the link works, in hours (default 168, at most 720)")
				uses := fs.Int("uses", 0, "how many channels may be connected through it (default 1, at most 50)")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					body := map[string]any{}
					setIf(body, "note", *note)
					if *platforms != "" {
						var ids []string
						for _, p := range strings.Split(*platforms, ",") {
							if p = strings.TrimSpace(p); p != "" {
								ids = append(ids, p)
							}
						}
						body["providers"] = ids
					}
					if *group != "" {
						groups, err := channelGroups(c, cl)
						if err != nil {
							return err
						}
						id, err := resolveGroupID(groups, *group)
						if err != nil {
							return err
						}
						body["group_id"] = id
					}
					if *hours != 0 {
						body["expires_in_hours"] = *hours
					}
					if *uses != 0 {
						body["max_uses"] = *uses
					}
					var l ChannelLink
					raw, err := cl.Post(c, "/v1/channel-links", body, &l)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Connect link %s (works until %s, %d %s):\n%s\n", l.ID, l.ExpiresAt, l.MaxUses,
						plural(l.MaxUses, "channel", "channels"), c.paint(bold, l.URL))
					fmt.Fprintln(c.Out, "Send it to the person who should connect the account; it is not shown again.")
					return nil
				}
			}, "staqly channels links create --note \"For ACME's X account\" --platforms x --group \"ACME\""),
			leaf("list", "", "List connect links with their status and uses", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var page struct{ Data []ChannelLink }
					raw, err := cl.Get(c, "/v1/channel-links", nil, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "STATUS", "USES", "EXPIRES", "NOTE")
					for _, l := range page.Data {
						t.row(l.ID, l.Status, fmt.Sprintf("%d/%d", l.Uses, l.MaxUses), l.ExpiresAt, orDash(truncate(l.Note, 40)))
					}
					t.flush("No connect links yet: staqly channels links create")
					return nil
				}
			}, "staqly channels links list"),
			leaf("revoke", "<id>", "Stop a connect link from working (channels connected through it stay)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/channel-links/"+url.PathEscape(args[0]), "connect link", args[0])
				}
			}, "staqly channels links revoke chl_oalirbz67y4m4cqxq5dw"),
		},
	}
}
