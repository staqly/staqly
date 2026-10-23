package providers

// The catalog: every platform staqly will publish to,
// listed so the dashboard shows the whole grid; the ones without an
// implementation yet are "soon". Limits are the platforms' published ones.

type soon struct{ Base }

func init() {
	img := []string{"image"}
	imgVid := []string{"image", "video"}
	for _, p := range []struct {
		id, name string
		auth     AuthKind
		limits   Limits
	}{
		{"x", "X", AuthOAuth2, Limits{MaxChars: 280, MaxMedia: 4, MediaKinds: imgVid}},
		{"linkedin", "LinkedIn", AuthOAuth2, Limits{MaxChars: 3000, MaxMedia: 9, MediaKinds: imgVid}},
		{"linkedin_page", "LinkedIn Page", AuthOAuth2, Limits{MaxChars: 3000, MaxMedia: 9, MediaKinds: imgVid}},
		{"reddit", "Reddit", AuthOAuth2, Limits{MaxChars: 40000, MaxMedia: 1, MediaKinds: imgVid, NeedsTitle: true}},
		{"instagram_business", "Instagram Business", AuthOAuth2, Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: imgVid, NeedsMedia: true}},
		{"facebook", "Facebook Page", AuthOAuth2, Limits{MaxChars: 63206, MaxMedia: 10, MediaKinds: imgVid}},
		{"threads", "Threads", AuthOAuth2, Limits{MaxChars: 500, MaxMedia: 10, MediaKinds: imgVid}},
		{"youtube", "YouTube", AuthOAuth2, Limits{MaxChars: 5000, MaxMedia: 1, MediaKinds: []string{"video"}, NeedsTitle: true, NeedsMedia: true}},
		{"google_business", "Google My Business", AuthOAuth2, Limits{MaxChars: 1500, MaxMedia: 1, MediaKinds: img}},
		{"tiktok", "TikTok", AuthOAuth2, Limits{MaxChars: 2200, MaxMedia: 1, MediaKinds: []string{"video"}, NeedsMedia: true}},
		{"pinterest", "Pinterest", AuthOAuth2, Limits{MaxChars: 500, MaxMedia: 1, MediaKinds: img, NeedsTitle: true, NeedsMedia: true, NeedsLink: true}},
		{"dribbble", "Dribbble", AuthOAuth2, Limits{MaxChars: 1000, MaxMedia: 1, MediaKinds: img, NeedsTitle: true, NeedsMedia: true}},
		{"discord", "Discord", AuthToken, Limits{MaxChars: 2000, MaxMedia: 10, MediaKinds: imgVid}},
		{"slack", "Slack", AuthOAuth2, Limits{MaxChars: 40000, MaxMedia: 10, MediaKinds: imgVid}},
		{"kick", "Kick", AuthOAuth2, Limits{MaxChars: 500}},
		{"twitch", "Twitch", AuthOAuth2, Limits{MaxChars: 500}},
		{"mastodon", "Mastodon", AuthOAuth2, Limits{MaxChars: 500, MaxMedia: 4, MediaKinds: imgVid}},
		{"bluesky", "Bluesky", AuthToken, Limits{MaxChars: 300, MaxMedia: 4, MediaKinds: img}},
		{"lemmy", "Lemmy", AuthToken, Limits{MaxChars: 10000, MaxMedia: 1, MediaKinds: img, NeedsTitle: true}},
		{"farcaster", "Farcaster", AuthToken, Limits{MaxChars: 320, MaxMedia: 2, MediaKinds: img}},
		{"telegram", "Telegram", AuthToken, Limits{MaxChars: 4096, MaxMedia: 10, MediaKinds: imgVid}},
		{"nostr", "Nostr", AuthToken, Limits{MaxChars: 0, MaxMedia: 4, MediaKinds: img}},
		{"medium", "Medium", AuthToken, Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: img, NeedsTitle: true}},
		{"devto", "Dev.to", AuthToken, Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: img, NeedsTitle: true}},
		{"hashnode", "Hashnode", AuthToken, Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: img, NeedsTitle: true}},
		{"wordpress", "WordPress", AuthToken, Limits{MaxChars: 0, MaxMedia: 1, MediaKinds: img, NeedsTitle: true}},
		{"listmonk", "ListMonk", AuthToken, Limits{MaxChars: 0, NeedsTitle: true}},
		{"moltbook", "Moltbook", AuthToken, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: img}},
		{"whop", "Whop", AuthOAuth2, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: imgVid}},
		{"skool", "Skool", AuthToken, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: img, NeedsTitle: true}},
		{"mewe", "MeWe", AuthToken, Limits{MaxChars: 5000, MaxMedia: 4, MediaKinds: imgVid}},
		{"tumblr", "Tumblr", AuthOAuth2, Limits{MaxChars: 4096, MaxMedia: 10, MediaKinds: imgVid}},
	} {
		if _, taken := registry[p.id]; taken {
			continue // an implementation registered itself
		}
		Register(soon{NewBase(p.id, p.name, p.auth, p.limits).WithSettings(soonSettings(p.id, p.limits)...)}, StatusSoon)
	}
}

// Shared settings of the soon platforms, named after what their publishing APIs take.
var (
	titleField = func(max int) Field {
		return Field{Key: "title", Label: "Title", Kind: "text", Required: true, MaxLength: max}
	}
	linkField = Field{Key: "link", Label: "Link", Kind: "url", Placeholder: "https://example.com/page"}
	// instagramFields: Instagram's post settings; a post's comments are its thread (the composer's comments).
	instagramFields = []Field{
		{Key: "post_type", Label: "Post as", Kind: "select", Default: "post", Options: []Option{
			{Value: "post", Label: "Feed post (several files make a carousel)"},
			{Value: "reel", Label: "Reel"},
			{Value: "story", Label: "Story"},
		}},
		{Key: "share_to_feed", Label: "Also show the reel in the feed", Kind: "boolean", Default: "true",
			When: &Condition{Key: "post_type", Value: "reel"}},
		{Key: "collaborators", Label: "Collaborators", Kind: "list", MaxItems: 3, Placeholder: "username",
			Help: "Up to 3 Instagram accounts invited as collaborators; the post shows on their profiles once they accept.",
			When: &Condition{Key: "post_type", Value: "post"}},
	}
	linkedinVisibility = Field{Key: "visibility", Label: "Who can see it", Kind: "select", Default: "public", Options: []Option{
		{Value: "public", Label: "Anyone"},
		{Value: "connections", Label: "Your connections only"},
	}}
)

// soonSettings: the per-post settings of a platform that is not built yet, so the composer shows and checks
// them already. A platform that needs a title or a link gets that field even when nothing else is listed.
func soonSettings(id string, l Limits) []Field {
	var fields []Field
	switch id {
	case "instagram_business":
		fields = instagramFields
	case "facebook":
		fields = []Field{{Key: "post_type", Label: "Post as", Kind: "select", Default: "post", Options: []Option{
			{Value: "post", Label: "Post"}, {Value: "reel", Label: "Reel"}, {Value: "story", Label: "Story"},
		}}}
	case "threads":
		fields = []Field{{Key: "reply_control", Label: "Who can reply", Kind: "select", Default: "everyone", Options: []Option{
			{Value: "everyone", Label: "Everyone"},
			{Value: "accounts_you_follow", Label: "Accounts you follow"},
			{Value: "mentioned_only", Label: "Only accounts you mention"},
		}}}
	case "tiktok":
		fields = []Field{
			{Key: "privacy_level", Label: "Who can watch", Kind: "select", Required: true, Options: []Option{
				{Value: "PUBLIC_TO_EVERYONE", Label: "Everyone"},
				{Value: "MUTUAL_FOLLOW_FRIENDS", Label: "Friends"},
				{Value: "FOLLOWER_OF_CREATOR", Label: "Followers"},
				{Value: "SELF_ONLY", Label: "Only you"},
			}, Help: "TikTok asks you to choose for every post."},
			{Key: "allow_comments", Label: "Allow comments", Kind: "boolean", Default: "true"},
			{Key: "allow_duet", Label: "Allow duets", Kind: "boolean", Default: "true"},
			{Key: "allow_stitch", Label: "Allow stitches", Kind: "boolean", Default: "true"},
			{Key: "ai_generated", Label: "AI-generated content", Kind: "boolean", Default: "false",
				Help: "TikTok labels the video as AI-generated."},
			{Key: "branded_content", Label: "Paid partnership (promotes a third party)", Kind: "boolean", Default: "false"},
			{Key: "your_brand", Label: "Promotes your own business", Kind: "boolean", Default: "false"},
		}
	case "youtube":
		fields = []Field{
			titleField(100),
			{Key: "privacy", Label: "Visibility", Kind: "select", Default: "public", Options: []Option{
				{Value: "public", Label: "Public"}, {Value: "unlisted", Label: "Unlisted"}, {Value: "private", Label: "Private"},
			}},
			{Key: "made_for_kids", Label: "Made for kids", Kind: "boolean", Default: "false",
				Help: "YouTube requires this answer for every video."},
			{Key: "tags", Label: "Tags", Kind: "list", MaxLength: 500, Placeholder: "running"},
			{Key: "category", Label: "Category", Kind: "select", Default: "22", Options: []Option{
				{Value: "1", Label: "Film & Animation"}, {Value: "2", Label: "Autos & Vehicles"}, {Value: "10", Label: "Music"},
				{Value: "15", Label: "Pets & Animals"}, {Value: "17", Label: "Sports"}, {Value: "19", Label: "Travel & Events"},
				{Value: "20", Label: "Gaming"}, {Value: "22", Label: "People & Blogs"}, {Value: "23", Label: "Comedy"},
				{Value: "24", Label: "Entertainment"}, {Value: "25", Label: "News & Politics"}, {Value: "26", Label: "Howto & Style"},
				{Value: "27", Label: "Education"}, {Value: "28", Label: "Science & Technology"},
			}},
			{Key: "playlist", Label: "Add to playlist", Kind: "url", Placeholder: "https://www.youtube.com/playlist?list=PL..."},
		}
	case "linkedin":
		fields = []Field{linkedinVisibility}
	case "pinterest":
		fields = []Field{
			titleField(100),
			{Key: "board", Label: "Board", Kind: "text", Required: true, Placeholder: "Marathon training",
				Help: "The board the pin is saved to."},
			{Key: "link", Label: "Link", Kind: "url", Required: true, Placeholder: "https://example.com/page"},
			{Key: "alt_text", Label: "Alt text", Kind: "textarea", MaxLength: 500,
				Help: "Describes the picture for people who use screen readers."},
		}
	case "reddit":
		fields = []Field{
			titleField(300),
			{Key: "subreddit", Label: "Subreddit", Kind: "text", Required: true, Placeholder: "r/running"},
			{Key: "flair", Label: "Flair", Kind: "text", Help: "Some subreddits ask for a flair; use the name they show."},
			{Key: "nsfw", Label: "Mark as NSFW", Kind: "boolean", Default: "false"},
			{Key: "spoiler", Label: "Mark as spoiler", Kind: "boolean", Default: "false"},
		}
	case "devto":
		fields = []Field{titleField(250),
			{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 4, Placeholder: "webdev"},
			{Key: "canonical_url", Label: "Original link", Kind: "url", Help: "Where the article first appeared, if anywhere."},
		}
	case "medium":
		fields = []Field{titleField(100),
			{Key: "tags", Label: "Tags", Kind: "list", MaxItems: 5, Placeholder: "productivity"},
			{Key: "canonical_url", Label: "Original link", Kind: "url", Help: "Where the article first appeared, if anywhere."},
		}
	case "lemmy":
		fields = []Field{titleField(200),
			{Key: "community", Label: "Community", Kind: "text", Required: true, Placeholder: "technology@lemmy.world"},
			linkField,
		}
	}
	has := func(key string) bool {
		for _, f := range fields {
			if f.Key == key {
				return true
			}
		}
		return false
	}
	if l.NeedsTitle && !has("title") {
		fields = append([]Field{titleField(0)}, fields...)
	}
	if l.NeedsLink && !has("link") {
		fields = append(fields, Field{Key: "link", Label: "Link", Kind: "url", Required: true})
	}
	return fields
}
