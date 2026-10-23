package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// YouTube over the YouTube Data API v3: Google's OAuth 2.0 (google.go) with refresh tokens, one channel per staqly
// channel (a Google account may own several through brand accounts, so YouTube is an AccountLister), the video
// sent through a resumable upload (videos.insert with uploadType=resumable), the post's thread as comments under
// the video. Apps Google has not audited upload every video as private: YouTube locks them private until the
// project passes its API audit.
type YouTube struct{ Base }

// Endpoints, variables so tests point them at httptest.
var (
	youtubeAPI = "https://www.googleapis.com" // /youtube/v3 and /upload/youtube/v3
	// youtubeChunk is how many bytes one upload request carries; YouTube wants a multiple of 256 KiB.
	youtubeChunk = 8 << 20
)

const youtubeScopes = googleIdentity +
	" https://www.googleapis.com/auth/youtube.upload" + // the video itself
	" https://www.googleapis.com/auth/youtube.readonly" + // the channels the sign-in owns
	" https://www.googleapis.com/auth/youtube.force-ssl" // comments and playlists

func init() {
	limits := Limits{MaxChars: 5000, MaxMedia: 1, MediaKinds: []string{"video"}, NeedsTitle: true, NeedsMedia: true}
	Register(YouTube{NewBase("youtube", "YouTube", AuthOAuth2, limits).WithSettings(soonSettings("youtube", limits)...)}, StatusAvailable)
}

var (
	_ AccountLister = YouTube{}
	_ Replier       = YouTube{}
	_ MetricsReader = YouTube{}
	_ AppChecker    = YouTube{}
)

func (y YouTube) Validate(p PostContent, s Settings) error {
	if err := CheckLimits(y.Limits(), p, s); err != nil {
		return err
	}
	for _, m := range p.Media {
		if !strings.HasPrefix(m.MimeType, "video/") {
			return invalid("media", "YouTube takes one video")
		}
	}
	// YouTube counts the description in bytes (an emoji is 4) and refuses angle brackets in it and in the title.
	if n := len(p.Text); n > 5000 {
		return invalid("content", fmt.Sprintf("YouTube takes at most 5,000 bytes of description (this one is %d; emoji and accented letters count as several)", n))
	}
	if strings.ContainsAny(p.Text, "<>") {
		return invalid("content", "YouTube refuses < and > in the description")
	}
	if strings.ContainsAny(s["title"], "<>") {
		return invalid("title", "YouTube refuses < and > in the title")
	}
	if utf8.RuneCountInString(strings.TrimSpace(s["title"])) == 0 {
		return invalid("title", "this channel needs a title")
	}
	if v := strings.TrimSpace(s["playlist"]); v != "" && youtubePlaylistID(v) == "" {
		return invalid("playlist", "use the playlist's link, the one with list=... in it")
	}
	return CheckFields(y.SettingsFields(), s)
}

// youtubePlaylistID reads the playlist id from its link (https://www.youtube.com/playlist?list=PL...).
func youtubePlaylistID(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	return u.Query().Get("list")
}

func (YouTube) AuthURL(app App, redirect, state string, extra map[string]string) (string, error) {
	return googleAuthURL(app, redirect, state, youtubeScopes, extra), nil
}

func (YouTube) Exchange(ctx context.Context, app App, redirect, code string, extra map[string]string) (Credentials, Profile, error) {
	return googleExchange(ctx, "YouTube", app, redirect, code, extra, "https://www.googleapis.com/auth/youtube.upload",
		"YouTube did not allow uploading videos: connect again and leave every permission Google asks for ticked")
}

// Refresh renews the hour-long access token with the refresh token.
func (YouTube) Refresh(ctx context.Context, app App, c Credentials) (Credentials, error) {
	return googleRefresh(ctx, "YouTube", app, c)
}

func (YouTube) CheckApp(ctx context.Context, app App, redirect string) (bool, error) {
	return googleCheckApp(ctx, "YouTube", app, redirect)
}

type youtubeChannel struct {
	ID      string `json:"id"`
	Snippet struct {
		Title      string `json:"title"`
		CustomURL  string `json:"customUrl"`
		Thumbnails struct {
			Default struct {
				URL string `json:"url"`
			} `json:"default"`
		} `json:"thumbnails"`
	} `json:"snippet"`
}

func (ch youtubeChannel) account() Account {
	a := Account{ID: ch.ID, Name: ch.Snippet.Title, Handle: ch.Snippet.CustomURL, AvatarURL: ch.Snippet.Thumbnails.Default.URL,
		ProfileURL: "https://www.youtube.com/channel/" + ch.ID}
	if a.Handle == "" {
		a.Handle = ch.ID
	}
	if strings.HasPrefix(a.Handle, "@") {
		a.ProfileURL = "https://www.youtube.com/" + a.Handle
	}
	return a
}

// Accounts lists the channels the sign-in owns. A Google account with brand accounts picks one of them on Google's
// consent screen, so the list is usually that one channel.
func (YouTube) Accounts(ctx context.Context, c Credentials) ([]Account, error) {
	var out struct {
		Items []youtubeChannel `json:"items"`
	}
	if err := googleGet(ctx, "YouTube", c["access_token"], youtubeAPI+"/youtube/v3/channels?part=snippet&mine=true&maxResults=50", &out); err != nil {
		return nil, err
	}
	if len(out.Items) == 0 {
		return nil, &Failure{Kind: ErrRejected, Msg: "This Google account has no YouTube channel: create one on youtube.com (or, when Google asks which account to use, pick the brand account that owns the channel), then connect again"}
	}
	list := make([]Account, len(out.Items))
	for i, ch := range out.Items {
		list[i] = ch.account()
	}
	return list, nil
}

// UseAccount keeps the sign-in and names the channel it publishes to.
func (y YouTube) UseAccount(ctx context.Context, c Credentials, accountID string) (Credentials, Profile, error) {
	list, err := y.Accounts(ctx, c)
	if err != nil {
		return nil, Profile{}, err
	}
	for _, a := range list {
		if a.ID == accountID {
			scope := c["scope"]
			return googleCopy(c, "channel_id", a.ID), Profile{Handle: a.Handle, Name: a.Name, AvatarURL: a.AvatarURL, ProfileURL: a.ProfileURL,
				NoComments: scope != "" && !strings.Contains(scope, "youtube.force-ssl")}, nil
		}
	}
	return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "This Google sign-in can't publish to that YouTube channel; connect again with the account that owns it"}
}

func (y YouTube) Publish(ctx context.Context, c Credentials, p PostContent, s Settings) (Result, error) {
	token := c["access_token"]
	if token == "" || c["channel_id"] == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "YouTube: no channel stored; reconnect the account"}
	}
	if len(p.Media) != 1 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "YouTube takes one video"}
	}
	playlist := youtubePlaylistID(s["playlist"])
	if playlist != "" {
		// Check the playlist before uploading: once the video is up, a failure could only be reported by uploading
		// it again.
		if err := y.checkPlaylist(ctx, token, c["channel_id"], playlist); err != nil {
			return Result{}, err
		}
	}
	privacy := s["privacy"]
	if privacy == "" {
		privacy = "public"
	}
	category := s["category"]
	if category == "" {
		category = "22"
	}
	snippet := map[string]any{"title": strings.TrimSpace(s["title"]), "description": p.Text, "categoryId": category}
	if tags := ListValues(s["tags"]); len(tags) > 0 {
		snippet["tags"] = tags
	}
	meta := map[string]any{
		"snippet": snippet,
		"status":  map[string]any{"privacyStatus": privacy, "selfDeclaredMadeForKids": s["made_for_kids"] == "true"},
	}
	id, err := y.upload(ctx, token, p.Media[0], meta)
	if err != nil {
		return Result{}, err
	}
	if playlist != "" {
		// The video is live: a playlist that refuses it now is not worth a second copy of the video, so the post
		// counts as published (the playlist was checked above).
		req, err := googleRequest(ctx, token, http.MethodPost, youtubeAPI+"/youtube/v3/playlistItems?part=snippet",
			map[string]any{"snippet": map[string]any{"playlistId": playlist, "resourceId": map[string]string{"kind": "youtube#video", "videoId": id}}})
		if err == nil {
			_, _ = googleDo(ctx, "YouTube", req, nil)
		}
	}
	return Result{ExternalID: id, URL: "https://www.youtube.com/watch?v=" + id}, nil
}

// checkPlaylist makes sure the playlist exists and belongs to the channel.
func (YouTube) checkPlaylist(ctx context.Context, token, channel, playlist string) error {
	var out struct {
		Items []struct {
			Snippet struct {
				ChannelID string `json:"channelId"`
			} `json:"snippet"`
		} `json:"items"`
	}
	q := url.Values{"part": {"snippet"}, "id": {playlist}}
	if err := googleGet(ctx, "YouTube", token, youtubeAPI+"/youtube/v3/playlists?"+q.Encode(), &out); err != nil {
		return err
	}
	if len(out.Items) == 0 {
		return &Failure{Kind: ErrRejected, Msg: "YouTube can't find the playlist " + playlist + "; check its link"}
	}
	if out.Items[0].Snippet.ChannelID != channel {
		return &Failure{Kind: ErrRejected, Msg: "The playlist " + playlist + " belongs to another YouTube channel; pick one of this channel's playlists"}
	}
	return nil
}

// upload sends the video through a resumable upload session and returns the video's id. YouTube creates the video
// only once the last byte has arrived, so an attempt that fails before that leaves nothing behind. A chunk whose
// answer is lost is followed by asking the session how far it got; only when that fails too after the last chunk
// may the video exist without staqly knowing.
func (YouTube) upload(ctx context.Context, token string, m MediaFile, meta map[string]any) (string, error) {
	if m.Size <= 0 {
		return "", &Failure{Kind: ErrRejected, Msg: "YouTube: the size of " + m.Name + " is unknown; upload the video again"}
	}
	req, err := googleRequest(ctx, token, http.MethodPost, youtubeAPI+"/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status", meta)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Upload-Content-Type", m.MimeType)
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(m.Size, 10))
	h, err := googleDo(ctx, "YouTube", req, nil)
	if err != nil {
		return "", err
	}
	session := h.Get("Location")
	// The token goes with every chunk: only to Google's upload host, over https.
	if !platformHost(session, youtubeAPI, "googleapis.com") {
		return "", &Failure{Msg: "YouTube did not start the upload; staqly tries again in a few minutes"}
	}

	f, err := m.Reader(ctx)
	if err != nil {
		return "", fmt.Errorf("YouTube: cannot read %s: %w", m.Name, err)
	}
	defer f.Close()
	var buf []byte // bytes from offset on that YouTube has not taken yet
	var offset int64
	for {
		if n := youtubeChunk - len(buf); n > 0 {
			more := make([]byte, n)
			got, err := io.ReadFull(f, more)
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
				return "", fmt.Errorf("YouTube: cannot read %s: %w", m.Name, err)
			}
			buf = append(buf, more[:got]...)
		}
		if len(buf) == 0 || offset+int64(len(buf)) > m.Size {
			return "", &Failure{Kind: ErrRejected, Msg: fmt.Sprintf("YouTube: %s is not the %d bytes it was stored as; upload the video again", m.Name, m.Size)}
		}
		last := offset+int64(len(buf)) == m.Size
		status, h, body, err := youtubePut(ctx, token, session, buf, fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(len(buf))-1, m.Size), m.MimeType)
		if err != nil {
			// The answer was lost: ask the session what it has.
			status, h, body, err = youtubePut(ctx, token, session, nil, fmt.Sprintf("bytes */%d", m.Size), "")
			if err != nil {
				if last {
					return "", &Failure{Kind: ErrRejected, Msg: "YouTube may have published the video, but its answer was lost (" + err.Error() + "); check the channel before posting it again"}
				}
				return "", err
			}
		}
		switch status {
		case http.StatusOK, http.StatusCreated:
			var v struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(body, &v) != nil || v.ID == "" {
				return "", &Failure{Kind: ErrRejected, Msg: "YouTube took the video but did not say its id; check the channel"}
			}
			return v.ID, nil
		case http.StatusPermanentRedirect: // 308 Resume Incomplete: "Range: bytes=0-<last byte it has>"
			next := youtubeCommitted(h.Get("Range"))
			if next < offset || next > offset+int64(len(buf)) || next >= m.Size {
				return "", &Failure{Msg: "YouTube lost track of the upload; staqly tries again in a few minutes"}
			}
			buf = buf[next-offset:]
			offset = next
		case http.StatusNotFound, http.StatusGone:
			return "", &Failure{Msg: "YouTube's upload session expired; staqly tries again in a few minutes"}
		default:
			return "", googleError("YouTube", status, body)
		}
	}
}

// youtubeCommitted reads the byte YouTube wants next from a Range header ("bytes=0-1048575"); none is 0.
func youtubeCommitted(r string) int64 {
	_, end, ok := strings.Cut(r, "-")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(end, 10, 64)
	if err != nil {
		return 0
	}
	return n + 1
}

// youtubePut sends one chunk (or, with no body, asks how far the session got) and returns YouTube's raw answer;
// an error means no answer came.
func youtubePut(ctx context.Context, token, session string, chunk []byte, contentRange, mime string) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, session, bytes.NewReader(chunk))
	if err != nil {
		return 0, nil, nil, err
	}
	req.ContentLength = int64(len(chunk))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Range", contentRange)
	if mime != "" {
		req.Header.Set("Content-Type", mime)
	}
	resp, err := httpClient(ctx).Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("YouTube could not be reached (%s)", req.URL.Host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, resp.Header, raw, nil
}

// Reply comments under the video. parentExternalID is the video or, from the second part on, the comment before
// ("comment:<video>:<thread>"); every part becomes a comment on the video itself, in order.
func (YouTube) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	token := c["access_token"]
	if token == "" || c["channel_id"] == "" {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "YouTube: no channel stored; reconnect the account"}
	}
	video := parentExternalID
	if rest, ok := strings.CutPrefix(parentExternalID, "comment:"); ok {
		video, _, _ = strings.Cut(rest, ":")
	}
	if video == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "YouTube: nothing to comment on (the video has no id)"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "YouTube comments take text only"}
	}
	req, err := googleRequest(ctx, token, http.MethodPost, youtubeAPI+"/youtube/v3/commentThreads?part=snippet", map[string]any{
		"snippet": map[string]any{"channelId": c["channel_id"], "videoId": video,
			"topLevelComment": map[string]any{"snippet": map[string]string{"textOriginal": p.Text}}},
	})
	if err != nil {
		return Result{}, err
	}
	var out struct {
		ID string `json:"id"`
	}
	if _, err := googleDo(ctx, "YouTube", req, &out); err != nil {
		var f *Failure
		if errors.As(err, &f) && strings.Contains(f.Msg, "disabled comments") {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "YouTube has comments turned off for this video (videos made for kids never take them). The video itself is live"}
		}
		return Result{}, err
	}
	return Result{ExternalID: "comment:" + video + ":" + out.ID, URL: "https://www.youtube.com/watch?v=" + video + "&lc=" + out.ID}, nil
}

// Metrics reads the video's public numbers: views (as impressions), likes and comments.
func (YouTube) Metrics(ctx context.Context, c Credentials, externalID string) (Metrics, error) {
	var out struct {
		Items []struct {
			Statistics struct {
				ViewCount    string `json:"viewCount"`
				LikeCount    string `json:"likeCount"`
				CommentCount string `json:"commentCount"`
			} `json:"statistics"`
		} `json:"items"`
	}
	q := url.Values{"part": {"statistics"}, "id": {externalID}}
	if err := googleGet(ctx, "YouTube", c["access_token"], youtubeAPI+"/youtube/v3/videos?"+q.Encode(), &out); err != nil {
		return Metrics{}, err
	}
	if len(out.Items) == 0 {
		return Metrics{}, &Failure{Kind: ErrRejected, Msg: "YouTube can't find the video; it may have been deleted"}
	}
	st := out.Items[0].Statistics
	num := func(v string) *int64 { // a hidden count (likes the channel hides) is absent, not zero
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil
		}
		return &n
	}
	return Metrics{Impressions: num(st.ViewCount), Likes: num(st.LikeCount), Replies: num(st.CommentCount)}, nil
}
