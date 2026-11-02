-- +goose Up
-- What each limit counts and what happens when it is reached, as the admin panel shows it. Values: -1 = no limit,
-- 0 = none allowed (data_export: 0 off, 1 on).
UPDATE limits SET description = 'AI spend a month, shared by all members: pictures, videos, the assistant and AI drafts, estimated from the providers'' list prices. Past it AI requests are refused until the next month. Stored in US cents.' WHERE key = 'ai_monthly_cents';
UPDATE limits SET description = 'Alerts set up at once (Telegram, Slack, Discord, ntfy or email). Past it a new alert is refused; existing ones keep working.' WHERE key = 'alerts';
UPDATE limits SET description = 'API keys at once, each with its own scopes (agents and automations). Past it a new key is refused; existing keys keep working.' WHERE key = 'api_keys';
UPDATE limits SET description = 'Connected channels at once: each connected account counts once, whatever the platform, your own automation included. Past it connecting another is refused.' WHERE key = 'channels';
UPDATE limits SET description = 'Backups and full exports: the workspace ZIP with its media files and the activity log export. Off: the workspace export has the data without files.' WHERE key = 'data_export';
UPDATE limits SET description = 'Posts to LinkedIn a calendar month (profiles and Pages; comments don''t count). LinkedIn''s API quota is per day and shared by every workspace, so each keeps to a fair share.' WHERE key = 'linkedin_posts_per_period';
UPDATE limits SET description = 'Of the LinkedIn posts a month, how many may carry a video (each also counts as a LinkedIn post).' WHERE key = 'linkedin_videos_per_period';
UPDATE limits SET description = 'Largest single file to upload to the media library (pictures, videos, design projects). Stored in bytes.' WHERE key = 'max_upload_bytes';
UPDATE limits SET description = 'Posts created a calendar month (UTC), by people, agents and schedules, drafts and duplicates included. Past it creating a post is refused until the next month.' WHERE key = 'posts_per_month';
UPDATE limits SET description = 'Media library storage: every file and design project kept. Past it uploads are refused until files are deleted. Stored in bytes.' WHERE key = 'storage_bytes';
UPDATE limits SET description = 'Signed webhook endpoints at once. Past it a new webhook is refused; existing ones keep delivering.' WHERE key = 'webhooks';
UPDATE limits SET description = 'X credits a calendar month, shared by all X channels: one per post or reply, 14 for a post with a clickable link. Bought X credits come on top.' WHERE key = 'x_posts_per_period';

-- +goose Down
SELECT 1;
