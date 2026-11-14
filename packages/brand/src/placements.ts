/**
 * Where a picture or video fits without cropping: the common shape per placement, by platform (provider ids
 * of the API's catalog). Ratios only: platforms change pixel sizes often. One table for the media library's
 * "Where it fits", the design editor's sizes and the aspect ratio tool on staqly.ai.
 *
 * These are recommendations. What a platform refuses (file size, formats, ratio range) is in the API's
 * provider catalog (image_rules on GET /v1/providers).
 */
export const PLACEMENTS: { provider: string; platform: string; name: string; w: number; h: number }[] = [
  { provider: 'instagram', platform: 'Instagram', name: 'Feed square', w: 1, h: 1 },
  { provider: 'instagram', platform: 'Instagram', name: 'Feed portrait', w: 4, h: 5 },
  { provider: 'instagram', platform: 'Instagram', name: 'Stories, Reels', w: 9, h: 16 },
  { provider: 'tiktok', platform: 'TikTok', name: 'Video', w: 9, h: 16 },
  { provider: 'youtube', platform: 'YouTube', name: 'Video', w: 16, h: 9 },
  { provider: 'youtube', platform: 'YouTube', name: 'Shorts', w: 9, h: 16 },
  { provider: 'x', platform: 'X', name: 'Post', w: 16, h: 9 },
  { provider: 'linkedin', platform: 'LinkedIn', name: 'Post', w: 1.91, h: 1 },
  { provider: 'facebook', platform: 'Facebook', name: 'Feed', w: 1, h: 1 },
  { provider: 'threads', platform: 'Threads', name: 'Post', w: 4, h: 5 },
  { provider: 'pinterest', platform: 'Pinterest', name: 'Pin', w: 2, h: 3 },
]
