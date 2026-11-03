/**
 * The "?" on a tile in Add channel, only where its name doesn't say it all: where a post lands when that is not the
 * obvious place (Pages only, chat messages, email), what an account must be, and which of two ways into one platform
 * this is. A platform whose name is the whole story (X, YouTube, Dev.to) has none.
 */
export const PROVIDER_HINTS: Record<string, string> = {
  custom: 'Hands each post to n8n, a script or an agent of your own, which reports back',
  linkedin: 'Posts to your personal LinkedIn profile',
  linkedin_page: 'Posts to LinkedIn company pages you administer',
  facebook: 'Posts, reels and stories on Facebook Pages you manage',
  instagram: 'Posts, reels and stories on an Instagram Business or Creator account',
  instagram_business: 'An Instagram account linked to a Facebook Page, signed in with Facebook',
  google_business: 'Updates on a Google Business Profile location',
  mastodon_token: 'Mastodon with an access token made on your server',
  bluesky_app_password: 'Bluesky with an app password, for accounts on a server of their own',
  kick: 'Chat messages in your Kick channel',
  twitch: 'Chat messages in your Twitch channel',
  discord: "Messages in a Discord server's channels, through the staqly bot",
  discord_webhook: 'Messages in one Discord channel, through a webhook made there',
  slack: 'Messages in Slack channels, through the staqly app',
  telegram: 'Messages in a Telegram channel or group',
  whop: "Posts to a Whop community's forum",
  moltbook: 'Posts as your AI agent on Moltbook, the social network for agents',
  medium: 'Stories on your Medium profile or a publication',
  wordpress: 'Posts on your self-hosted WordPress site',
  listmonk: 'Email campaigns from your self-hosted listmonk',
}
