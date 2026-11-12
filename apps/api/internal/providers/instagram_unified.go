package providers

// InstagramUnified is Instagram's Unified login as a tile of its own, "Instagram (Professional)": offered next to
// Instagram login ("Instagram (Standalone)") when the server has the Instagram app's own keys, so people can also
// sign in with Facebook and reach accounts a business portfolio owns. It signs in with the Meta app's keys and the
// same configuration (PROVIDER_INSTAGRAM_CONFIG_ID). Without Instagram login the one "Instagram" tile already is
// Unified login, and this one stays hidden. Everything else is Instagram's (embedded).
type InstagramUnified struct{ Instagram }

func init() {
	Register(InstagramUnified{Instagram{NewBase("instagram_unified", "Instagram (Professional)", AuthOAuth2,
		Limits{MaxChars: 2200, MaxMedia: 10, MediaKinds: []string{"image", "video"}, NeedsMedia: true, CommentsTextOnly: true})}}, StatusAvailable)
}
