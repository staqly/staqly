package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/staqly/staqly/apps/api/internal/db"
	"github.com/staqly/staqly/apps/api/internal/providers"
)

// Personal notifications: in a cloud account, each person picks per event whether they get an email (to the
// address they sign in with) and whether it shows in the bell, and whether post events cover only their own
// posts (default) or every post in the workspace. The emit hook only queues a notify_people job for the event
// (inside its transaction); that job works out who gets it and queues one notify_person job per person, which
// sends one email (the job key makes it once per event and person). Workspace alerts (Automation, Alerts) are
// separate and unchanged.

// PersonalEvents are the events a person can get, in the order the settings list them.
var PersonalEvents = []string{"post.published", "post.failed", "post.feedback_added", "channel.needs_reconnect",
	"schedule.posts_requested", "invitation.accepted"}

// Who an event is for: posts go to their author (and to everyone who chose every post); the rest to the
// people who look after the workspace (owners and admins).
var postEvents = map[string]bool{"post.published": true, "post.failed": true, "post.feedback_added": true}

type NotificationChoice struct {
	Event string `json:"event"`
	Email bool   `json:"email"`
	InApp bool   `json:"in_app"`
}

type NotificationPrefs struct {
	// Scope: "mine" (posts I wrote, or that my API keys wrote) or "all" (every post in the workspaces I'm in).
	Scope  string               `json:"scope"`
	Events []NotificationChoice `json:"events"`
	// Read-only: where emails go and whether this server can send them.
	Email     string `json:"email"`
	MailReady bool   `json:"mail_ready"`
}

type storedPrefs struct {
	Scope  string                        `json:"scope,omitempty"`
	Events map[string]NotificationChoice `json:"events,omitempty"`
}

func parsePrefs(raw string) storedPrefs {
	var p storedPrefs
	_ = json.Unmarshal([]byte(raw), &p)
	if p.Scope != "all" {
		p.Scope = "mine"
	}
	return p
}

// choice is the person's setting for an event; every event is on by default (email and bell).
func (p storedPrefs) choice(event string) NotificationChoice {
	if c, ok := p.Events[event]; ok {
		c.Event = event
		return c
	}
	return NotificationChoice{Event: event, Email: true, InApp: true}
}

func (s *Service) personFor(ctx context.Context, p Principal) (db.User, error) {
	if p.UserID == "" || p.Kind == PrincipalAPIKey {
		return db.User{}, &Error{Code: "sign_in_with_account", Status: 403, Message: "notification settings belong to a signed-in person"}
	}
	return s.Q.GetUser(ctx, p.UserID)
}

// GetNotificationPrefs returns the signed-in person's choices, every event listed.
func (s *Service) GetNotificationPrefs(ctx context.Context, p Principal) (NotificationPrefs, error) {
	u, err := s.personFor(ctx, p)
	if err != nil {
		return NotificationPrefs{}, err
	}
	st := parsePrefs(u.NotificationPrefs)
	out := NotificationPrefs{Scope: st.Scope, Email: u.Email, MailReady: s.mailConfigured()}
	for _, e := range PersonalEvents {
		out.Events = append(out.Events, st.choice(e))
	}
	return out, nil
}

// SetNotificationPrefs replaces the person's choices (unknown events are refused).
func (s *Service) SetNotificationPrefs(ctx context.Context, p Principal, in NotificationPrefs) (NotificationPrefs, error) {
	u, err := s.personFor(ctx, p)
	if err != nil {
		return NotificationPrefs{}, err
	}
	st := parsePrefs(u.NotificationPrefs)
	if in.Scope != "" {
		if in.Scope != "mine" && in.Scope != "all" {
			return NotificationPrefs{}, invalid("scope", "must be mine or all")
		}
		st.Scope = in.Scope
	}
	if st.Events == nil {
		st.Events = map[string]NotificationChoice{}
	}
	for _, c := range in.Events {
		if !slices.Contains(PersonalEvents, c.Event) {
			return NotificationPrefs{}, invalid("events", "unknown event "+c.Event)
		}
		st.Events[c.Event] = NotificationChoice{Email: c.Email, InApp: c.InApp}
	}
	if err := s.Q.SetUserNotificationPrefs(ctx, db.SetUserNotificationPrefsParams{NotificationPrefs: mustJSON(st), UpdatedAt: s.now(), ID: u.ID}); err != nil {
		return NotificationPrefs{}, err
	}
	return s.GetNotificationPrefs(ctx, p)
}

// ---- unsubscribe links ----

// unsubscribeToken is signed with the server's secret key: "<user id>:<event>" and an HMAC, so a link turns off
// one kind of email for one person without signing in, and can't be forged for anyone else.
func (s *Service) unsubscribeToken(userID, event string) string {
	msg := userID + ":" + event
	return base64.RawURLEncoding.EncodeToString([]byte(msg)) + "." + base64.RawURLEncoding.EncodeToString(s.unsubscribeMAC(msg))
}

func (s *Service) unsubscribeMAC(msg string) []byte {
	mac := hmac.New(sha256.New, s.Cfg.SecretKey)
	mac.Write([]byte("unsubscribe:" + msg))
	return mac.Sum(nil)
}

// Unsubscribe turns off the email for the token's person and event. It answers which event it was.
func (s *Service) Unsubscribe(ctx context.Context, token string) (string, error) {
	bad := &Error{Code: "invalid_link", Status: 400, Message: "this unsubscribe link is not valid"}
	raw, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok {
		return "", bad
	}
	msgB, err1 := base64.RawURLEncoding.DecodeString(raw)
	sigB, err2 := base64.RawURLEncoding.DecodeString(sig)
	if err1 != nil || err2 != nil || !hmac.Equal(sigB, s.unsubscribeMAC(string(msgB))) {
		return "", bad
	}
	userID, event, _ := strings.Cut(string(msgB), ":")
	if !slices.Contains(PersonalEvents, event) {
		return "", bad
	}
	u, err := s.Q.GetUser(ctx, userID)
	if err != nil {
		return "", bad
	}
	st := parsePrefs(u.NotificationPrefs)
	c := st.choice(event)
	c.Email = false
	if st.Events == nil {
		st.Events = map[string]NotificationChoice{}
	}
	st.Events[event] = NotificationChoice{Email: false, InApp: c.InApp}
	if err := s.Q.SetUserNotificationPrefs(ctx, db.SetUserNotificationPrefsParams{NotificationPrefs: mustJSON(st), UpdatedAt: s.now(), ID: u.ID}); err != nil {
		return "", err
	}
	return event, nil
}

// ---- delivery ----

func init() {
	RegisterEmitHook(personalNotifyHook)
}

// personalNotifyHook queues the fan-out for an event people can be told about (inside the event's
// transaction, so a rolled-back event tells nobody). Nothing is queued without accounts or mail.
func personalNotifyHook(ctx context.Context, s *Service, q *db.Queries, ev db.Event) error {
	if !slices.Contains(PersonalEvents, ev.Type) || !s.AccountsEnabled() || !s.mailConfigured() {
		return nil
	}
	return s.enqueue(ctx, q, ev.WorkspaceID, "notify_people", "notify_people:"+ev.ID, map[string]any{"event_id": ev.ID}, s.Now(), 0)
}

type notifyRecipient struct {
	user db.User
	role string
}

// workspacePeople: the members with their roles, and the account owner (an owner everywhere in the account).
func (s *Service) workspacePeople(ctx context.Context, workspaceID string) ([]notifyRecipient, error) {
	members, err := s.Q.ListMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	var out []notifyRecipient
	seen := map[string]bool{}
	ws, err := s.Q.GetWorkspace(ctx, workspaceID)
	if err == nil && ws.AccountID.Valid {
		if acc, err := s.Q.GetAccount(ctx, ws.AccountID.String); err == nil {
			if u, err := s.Q.GetUser(ctx, acc.OwnerUserID); err == nil {
				out = append(out, notifyRecipient{user: u, role: RoleOwner})
				seen[u.ID] = true
			}
		}
	}
	for _, m := range members {
		if seen[m.UserID] {
			continue
		}
		u, err := s.Q.GetUser(ctx, m.UserID)
		if err != nil {
			continue
		}
		seen[u.ID] = true
		out = append(out, notifyRecipient{user: u, role: m.Role})
	}
	return out, nil
}

// recipients decides who gets an event: post events go to the post's author and to editors and up who chose
// every post; the rest to owners and admins. The person who caused it (an accepted invitation's new member)
// is left out. Only people with the email on for that event.
func (s *Service) recipients(ctx context.Context, ev db.Event) ([]db.User, error) {
	people, err := s.workspacePeople(ctx, ev.WorkspaceID)
	if err != nil {
		return nil, err
	}
	author := ""
	if postEvents[ev.Type] && ev.ItemID.Valid {
		if row, err := s.Q.GetPostByID(ctx, ev.ItemID.String); err == nil && row.WorkspaceID == ev.WorkspaceID {
			author = row.CreatedByUser
		}
	}
	var data struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal([]byte(ev.Data), &data)
	var out []db.User
	for _, r := range people {
		st := parsePrefs(r.user.NotificationPrefs)
		if !st.choice(ev.Type).Email || r.user.Email == "" {
			continue
		}
		in := false
		if postEvents[ev.Type] {
			in = r.user.ID == author || (st.Scope == "all" && roleRank(r.role) >= roleRank(RoleEditor))
		} else {
			in = r.role == RoleOwner || r.role == RoleAdmin
		}
		if in && r.user.ID != data.UserID {
			out = append(out, r.user)
		}
	}
	return out, nil
}

// runNotifyPeople is the notify_people job: one notify_person job per person who gets the event.
func (s *Service) runNotifyPeople(ctx context.Context, eventID string) error {
	ev, err := s.Q.GetEvent(ctx, eventID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	users, err := s.recipients(ctx, ev)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(q *db.Queries) error {
		for _, u := range users {
			if err := s.enqueue(ctx, q, ev.WorkspaceID, "notify_person", "notify_person:"+ev.ID+":"+u.ID,
				map[string]any{"event_id": ev.ID, "user_id": u.ID}, s.Now(), 0); err != nil {
				return err
			}
		}
		return nil
	})
}

// runNotifyPerson is the notify_person job: one email for one event and one person (checked again: they
// may have turned it off or left meanwhile).
func (s *Service) runNotifyPerson(ctx context.Context, eventID, userID string) error {
	ev, err := s.Q.GetEvent(ctx, eventID)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	users, err := s.recipients(ctx, ev)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(users, func(u db.User) bool { return u.ID == userID })
	if i < 0 {
		return nil
	}
	u := users[i]
	subject, d := s.personalEmail(ctx, ev, u)
	if subject == "" {
		return nil
	}
	unsub := s.webURL() + "/unsubscribe#" + s.unsubscribeToken(u.ID, ev.Type)
	oneClick := s.webURL() + "/api/v1/notifications/unsubscribe?token=" + s.unsubscribeToken(u.ID, ev.Type)
	d.Prefs = &emailLink{Text: "Change notification preferences", URL: s.webURL() + "/settings/account#notifications"}
	d.Unsub = &emailLink{Text: "Unsubscribe from these emails", URL: unsub}
	return s.sendSystemEmailDoc(ctx, u.Email, subject, d, map[string]string{
		"List-Unsubscribe":      "<" + oneClick + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	})
}

// personalEmail writes the email for an event; "" when there is nothing to say (the post is gone).
func (s *Service) personalEmail(ctx context.Context, ev db.Event, u db.User) (string, emailDoc) {
	ws, _ := s.Q.GetWorkspace(ctx, ev.WorkspaceID)
	because := func(what string) []string {
		return []string{fmt.Sprintf("You get this because %s in the workspace %s.", what, ws.Name)}
	}
	var data map[string]any
	_ = json.Unmarshal([]byte(ev.Data), &data)
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }

	if postEvents[ev.Type] {
		if !ev.ItemID.Valid {
			return "", emailDoc{}
		}
		post, _, err := s.loadPost(ctx, s.Q, ev.WorkspaceID, ev.ItemID.String)
		if err != nil {
			return "", emailDoc{}
		}
		why := "it's your post"
		if row, err := s.Q.GetPostByID(ctx, post.ID); err != nil || row.CreatedByUser != u.ID {
			why = "you follow every post"
		}
		d := emailDoc{Quote: truncateRunes(firstLine(post.Content), 280), Button: &emailLink{Text: "View post", URL: s.webURL() + "/posts/" + post.ID}}
		var names []string
		for _, t := range post.Targets {
			name := t.Provider
			if e, ok := providers.Get(t.Provider); ok {
				name = e.Name()
			}
			r := emailRow{Label: name + " · " + t.ChannelName}
			switch {
			case t.Status == "published":
				r.Detail = "Published"
				if t.ExternalURL != "" {
					r.Link = &emailLink{Text: "Open on " + name, URL: t.ExternalURL}
				}
				if ev.Type == "post.published" {
					names = appendUnique(names, name)
				}
			case t.Status == "failed":
				r.Detail, r.Failed = truncateRunes(firstNonEmpty(t.Error, "Publishing failed"), 300), true
				if ev.Type == "post.failed" {
					names = appendUnique(names, name)
				}
			default:
				r.Detail = "Not published"
			}
			d.Rows = append(d.Rows, r)
		}
		switch ev.Type {
		case "post.published":
			d.Title = "Your post was published on " + joinAnd(names)
			d.Paragraphs = []string{"It went out " + publishedWhere(len(post.Targets)) + "."}
		case "post.failed":
			d.Title = "Publishing failed on " + joinAnd(names)
			d.Paragraphs = []string{"Staqly tried every channel and some of them refused. Fix what the channel says and retry it from the post."}
		case "post.feedback_added":
			name, comment := firstNonEmpty(str(data, "author_name"), "A reviewer"), str(data, "body")
			d.Title = name + " left feedback on your post"
			d.Paragraphs = []string{truncateRunes(comment, 600)}
			d.Rows = nil
		}
		d.Preheader = d.Title
		d.Footer = because(why)
		return d.Title, d
	}

	d := emailDoc{}
	switch ev.Type {
	case "channel.needs_reconnect":
		ch, _ := data["channel"].(map[string]any)
		d.Title = firstNonEmpty(str(ch, "name"), "A channel") + " needs reconnecting"
		d.Paragraphs = []string{"Staqly can't publish to this channel until someone signs in to it again. Posts for it wait meanwhile.",
			truncateRunes(str(data, "error"), 300)}
		d.Button = &emailLink{Text: "Reconnect the channel", URL: s.webURL() + "/channels"}
	case "schedule.posts_requested":
		sch, _ := data["schedule"].(map[string]any)
		d.Title = firstNonEmpty(str(sch, "name"), "A schedule") + " is out of posts"
		d.Paragraphs = []string{"Its collection has nothing waiting, so this run published nothing. Write a few posts or ask your agent for more."}
		d.Button = &emailLink{Text: "Open the calendar", URL: s.webURL() + "/schedule"}
	case "invitation.accepted":
		who := firstNonEmpty(str(data, "email"), "Someone")
		if joined, err := s.Q.GetUser(ctx, str(data, "user_id")); err == nil && joined.Name != "" {
			who = joined.Name
		}
		d.Title = who + " joined " + ws.Name
		d.Paragraphs = []string{"They accepted the invitation and can now work in this workspace with the role you gave them."}
		d.Button = &emailLink{Text: "See the members", URL: s.webURL() + "/settings/members"}
	default:
		return "", emailDoc{}
	}
	d.Paragraphs = slices.DeleteFunc(d.Paragraphs, func(p string) bool { return strings.TrimSpace(p) == "" })
	d.Preheader = d.Title
	d.Footer = because("you look after it")
	return d.Title, d
}

func publishedWhere(n int) string {
	if n == 1 {
		return "on its channel"
	}
	return fmt.Sprintf("on its %d channels", n)
}

func appendUnique(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// joinAnd: "X", "X and LinkedIn", "X, LinkedIn and Threads".
func joinAnd(list []string) string {
	switch len(list) {
	case 0:
		return "your channels"
	case 1:
		return list[0]
	}
	return strings.Join(list[:len(list)-1], ", ") + " and " + list[len(list)-1]
}
