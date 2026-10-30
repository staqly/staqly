// Screenshots of the real dashboard (public/screenshots/<name>-<width>.webp + <name>.jpg). They were taken of a
// demo workspace ("Juniper Studio") on `staqly serve` built from this repository, at 1440 x 900 CSS px, 2x.

// Each is cropped to the part of the screen that matters:
// [the WebP widths, the JPEG's width and height].
const SHOTS = {
  posts: [[640, 1060, 1744], 1060, 1094],
  composer: [[640, 1060, 2048], 1060, 932],
  calendar: [[800, 1440, 2160], 1440, 900],
  'calendar-week': [[640, 1184, 2368], 1184, 620],
  assistant: [[640, 1060, 1480], 1060, 1289],
  'design-editor': [[800, 1440, 2160], 1440, 900],
  'preview-link': [[640, 1110, 2220], 1110, 820],
  analytics: [[640, 1150, 2300], 1150, 900],
} satisfies Record<string, [number[], number, number]>

type Desktop = keyof typeof SHOTS

/**
 * A dashboard screenshot in a window frame. `sizes` is the width it shows at; `eager` for the one above the fold;
 * `crop` (an aspect ratio and object position) shows only part of a tall screenshot.
 */
export function Shot({
  name,
  alt,
  sizes = '(min-width: 72rem) 1088px, calc(100vw - 2.5rem)',
  eager,
  crop,
  className = '',
}: {
  name: Desktop
  alt: string
  sizes?: string
  eager?: boolean
  crop?: string
  className?: string
}) {
  const [widths, width, height] = SHOTS[name]

  return (
    <figure
      className={`overflow-hidden rounded-xl border border-white/10 bg-[#0d0e11] shadow-[0_40px_120px_-40px_rgb(0_0_0/0.8),0_0_0_1px_rgb(255_255_255/0.03)] sm:rounded-2xl ${className}`}
    >
      <div aria-hidden className="flex h-7 items-center gap-1.5 border-b border-white/8 px-3 sm:h-9 sm:px-4">
        <span className="size-2 rounded-full bg-white/15 sm:size-2.5" />
        <span className="size-2 rounded-full bg-white/15 sm:size-2.5" />
        <span className="size-2 rounded-full bg-white/15 sm:size-2.5" />
      </div>
      <picture>
        <source
          type="image/webp"
          srcSet={widths.map((w) => `/screenshots/${name}-${w}.webp ${w}w`).join(', ')}
          sizes={sizes}
        />
        <img
          src={`/screenshots/${name}.jpg`}
          alt={alt}
          width={width}
          height={height}
          loading={eager ? 'eager' : 'lazy'}
          fetchPriority={eager ? 'high' : undefined}
          decoding="async"
          className={`block w-full ${crop ? `object-cover ${crop}` : 'h-auto'}`}
        />
      </picture>
    </figure>
  )
}

/** The dashboard on a phone: the schedule as a list, with the bottom tab bar. */
export function PhoneShot({ className = '' }: { className?: string }) {
  return (
    <figure
      className={`overflow-hidden rounded-[2rem] border-[6px] border-[#26282e] bg-[#0d0e11] shadow-[0_30px_80px_-20px_rgb(0_0_0/0.85)] ${className}`}
    >
      <picture>
        <source
          type="image/webp"
          srcSet="/screenshots/phone-schedule-390.webp 390w, /screenshots/phone-schedule-780.webp 780w"
          sizes="200px"
        />
        <img
          src="/screenshots/phone-schedule.jpg"
          alt="The staqly schedule on a phone: Monday's posts as a list with their channels and times, and a tab bar at the bottom"
          width={390}
          height={844}
          loading="lazy"
          decoding="async"
          className="block h-auto w-full"
        />
      </picture>
    </figure>
  )
}
