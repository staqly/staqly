// The free tools under staqly.ai/tools: one entry per page (index cards, sitemap, llms.txt, the home teaser) and a
// link each in the site footer, which staqly.ai and the docs share (site-footer.tsx).

/** `short` is the name in the footer and menus. */
export type Tool = {
  slug: string
  title: string
  short: string
  summary: string
  icon: 'clock' | 'type' | 'link' | 'check' | 'ratio' | 'grid' | 'slides' | 'format' | 'title'
}

export const TOOLS: Tool[] = [
  {
    slug: 'best-time-to-post',
    title: 'Best time to post',
    short: 'Best time to post',
    summary:
      'A weekly heatmap of strong posting hours for Instagram, TikTok, YouTube, X, LinkedIn and more, in your audience’s time zone and yours.',
    icon: 'clock',
  },
  {
    slug: 'character-counter',
    title: 'Character counter for every platform',
    short: 'Character counter',
    summary:
      'Paste a caption and see at once where it fits: Instagram, TikTok, YouTube, X, Threads, LinkedIn, Bluesky and more, links handled the way X wants them.',
    icon: 'type',
  },
  {
    slug: 'aspect-ratio-checker',
    title: 'Aspect ratio checker',
    short: 'Aspect ratio checker',
    summary:
      'Drop a picture or video, or type its size, and see how it fits Instagram, TikTok, YouTube, X, LinkedIn, Facebook, Threads and Pinterest, with a live crop preview.',
    icon: 'ratio',
  },
  {
    slug: 'schedule-validator',
    title: 'Schedule validator',
    short: 'Schedule validator',
    summary:
      'Check a posting plan before it runs: posts per day for Instagram, TikTok, YouTube and more, posts too close together, quiet days, hours your audience sleeps and clock changes.',
    icon: 'check',
  },
  {
    slug: 'link-to-plain-text',
    title: 'Links to plain text',
    short: 'Links to plain text',
    summary:
      'Turn links into readable text that no platform treats as a link (example[.]com), take them out, or turn plain text back into links.',
    icon: 'link',
  },
  {
    slug: 'instagram-grid-maker',
    title: 'Instagram grid maker',
    short: 'Instagram grid maker',
    summary:
      'Cut one picture into 3 to 15 posts that join up on your Instagram profile, as 4:5, 3:4 or square posts, numbered in the order you post them.',
    icon: 'grid',
  },
  {
    slug: 'instagram-carousel-splitter',
    title: 'Instagram carousel splitter',
    short: 'Carousel splitter',
    summary:
      'Turn a wide picture or panorama into up to 20 carousel slides that run on seamlessly when swiped, with a swipe preview and a ZIP of every slide.',
    icon: 'slides',
  },
  {
    slug: 'linkedin-text-formatter',
    title: 'LinkedIn text formatter',
    short: 'LinkedIn text formatter',
    summary:
      'Bold, italic, underline, strikethrough and lists for LinkedIn, X, Instagram and Threads, which have no formatting of their own, with a preview of where the feed folds your post.',
    icon: 'format',
  },
  {
    slug: 'youtube-title-checker',
    title: 'YouTube title checker',
    short: 'YouTube title checker',
    summary:
      'Check a video title against YouTube’s 100 characters and see it on home, search and up next, with your thumbnail, before you publish.',
    icon: 'title',
  },
]
