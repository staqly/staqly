package providers

// The catalog (docs/posts-pivot.md): every platform staqly will publish to,
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
		{"instagram", "Instagram (Facebook Business)", AuthOAuth2, Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: imgVid, NeedsMedia: true}},
		{"instagram_standalone", "Instagram (Standalone)", AuthOAuth2, Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: imgVid, NeedsMedia: true}},
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
		{"vk", "VK", AuthOAuth2, Limits{MaxChars: 16000, MaxMedia: 10, MediaKinds: imgVid}},
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
		Register(soon{NewBase(p.id, p.name, p.auth, p.limits)}, StatusSoon)
	}
}
