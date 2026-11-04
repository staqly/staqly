/**
 * The "?" on a tile in Add channel, kept for the few where connecting goes wrong without it: what an account must be
 * (Instagram), which of two ways into one platform this is, and where a post lands when the name doesn't say
 * (Whop's forum, your own automation). Every other platform has none.
 */
export const PROVIDER_HINTS: Record<string, string> = {
  custom: 'Hands each post to n8n, a script or an agent of your own, which reports back',
  instagram: 'Posts, reels and stories on an Instagram Business or Creator account',
  instagram_business: 'An Instagram account linked to a Facebook Page, signed in with Facebook',
  mastodon_token: 'Mastodon with an access token made on your server',
  bluesky_app_password: 'Bluesky with an app password, for accounts on a server of their own',
  discord_webhook: 'Messages in one Discord channel, through a webhook made there',
  whop: "Posts to a Whop community's forum",
  whop_key: "Posts to a Whop community's forum, connected with an Account API key",
  hashnode: 'Needs a blog on Hashnode Pro: since May 2026 Hashnode opens its API only to Pro blogs',
}
