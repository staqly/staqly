// Ready-made designs: type, shapes and colour only, so they work offline and need no stock photos.
import { uid, type Design, type El, type Fill, type ShapeEl, type ShapeKind, type TextEl } from './doc'

function t(
  text: string,
  x: number,
  y: number,
  width: number,
  font: string,
  size: number,
  fill: string,
  o: Partial<TextEl> = {},
): TextEl {
  return {
    id: uid(),
    type: 'text',
    text,
    x,
    y,
    width,
    height: size * 1.2,
    rotation: 0,
    opacity: 1,
    fontFamily: font,
    fontSize: size,
    bold: false,
    italic: false,
    underline: false,
    fill,
    align: 'center',
    lineHeight: 1.15,
    letterSpacing: 0,
    ...o,
  }
}

function s(
  shape: ShapeKind,
  x: number,
  y: number,
  width: number,
  height: number,
  fill: string,
  o: Partial<ShapeEl> = {},
): ShapeEl {
  return {
    id: uid(),
    type: 'shape',
    shape,
    x,
    y,
    width,
    height,
    rotation: 0,
    opacity: 1,
    fill,
    stroke: '#000000',
    strokeWidth: 0,
    radius: 0,
    ...o,
  }
}

const grad = (from: string, to: string, angle = 135): Fill => ({ kind: 'gradient', from, to, angle })
const color = (c: string): Fill => ({ kind: 'color', color: c })

function d(name: string, width: number, height: number, background: Fill, elements: El[]): Design {
  return { version: 1, name, width, height, pages: [{ id: uid(), background, elements }] }
}

/** A carousel: one design, a page per slide. */
function carousel(name: string, width: number, height: number, slides: [Fill, El[]][]): Design {
  return {
    version: 1,
    name,
    width,
    height,
    pages: slides.map(([background, elements]) => ({ id: uid(), background, elements })),
  }
}

/** Fresh copies (new element ids) every time, so two uses of a template never share ids. */
export function templates(): Design[] {
  return [
    d('Quote', 1080, 1080, grad('#0f172a', '#312e81'), [
      t('“', 90, 40, 300, 'Playfair Display', 360, '#818cf8', { align: 'left' }),
      t('Simplicity is the ultimate sophistication.', 120, 390, 840, 'Playfair Display', 76, '#ffffff', {
        lineHeight: 1.2,
      }),
      s('rect', 490, 760, 100, 6, '#818cf8', { radius: 3 }),
      t('Leonardo da Vinci', 140, 800, 800, 'Inter', 34, '#c7d2fe', { letterSpacing: 2 }),
    ]),
    d('Big news', 1080, 1350, color('#fef3c7'), [
      s('ellipse', -160, -160, 520, 520, '#fde68a'),
      s('ellipse', 760, 1020, 480, 480, '#fcd34d'),
      t('BIG NEWS', 90, 400, 900, 'Bebas Neue', 230, '#111827', { letterSpacing: 4 }),
      t(
        'Something new is coming to our shop next week. Be the first to see it.',
        140,
        680,
        800,
        'Poppins',
        44,
        '#374151',
        { lineHeight: 1.4 },
      ),
      s('rect', 340, 960, 400, 110, '#111827', { radius: 55 }),
      t('Learn more', 340, 988, 400, 'Poppins', 44, '#ffffff', { bold: true }),
    ]),
    d('Sale', 1080, 1080, grad('#f43f5e', '#f97316'), [
      t('SUMMER SALE', 60, 130, 960, 'Anton', 120, '#ffffff', { letterSpacing: 6 }),
      t('-50%', 60, 300, 960, 'Anton', 380, '#fff7ed', { stroke: { color: '#9f1239', width: 6 } }),
      s('rect', 290, 840, 500, 100, '#ffffff', { radius: 50 }),
      t('Only this weekend', 290, 866, 500, 'Inter', 40, '#e11d48', { bold: true }),
    ]),
    d('Event story', 1080, 1920, color('#111827'), [
      s('star', 820, 160, 140, 140, '#facc15'),
      s('star', 120, 1560, 90, 90, '#f472b6'),
      t('LIVE WORKSHOP', 90, 520, 900, 'Montserrat', 64, '#a5b4fc', { bold: true, letterSpacing: 8 }),
      t('Design for social media', 90, 640, 900, 'Montserrat', 120, '#ffffff', { bold: true, lineHeight: 1.05 }),
      s('rect', 90, 1010, 900, 2, '#374151'),
      t('Thursday, 14 November\n18:00 CET · Online', 90, 1070, 900, 'Inter', 52, '#e5e7eb', { lineHeight: 1.5 }),
      s('rect', 240, 1360, 600, 130, '#6366f1', { radius: 65 }),
      t('Save your seat', 240, 1394, 600, 'Inter', 52, '#ffffff', { bold: true }),
    ]),
    d('Tips', 1080, 1350, color('#ffffff'), [
      s('rect', 0, 0, 28, 1350, '#6366f1'),
      t('5 tips for\nbetter posts', 110, 120, 860, 'Space Grotesk', 104, '#111827', {
        bold: true,
        align: 'left',
        lineHeight: 1.05,
      }),
      t(
        '1. Lead with the point\n2. One idea per post\n3. Show, then tell\n4. Ask a question\n5. Post when people read',
        110,
        480,
        860,
        'Inter',
        52,
        '#374151',
        { align: 'left', lineHeight: 1.8 },
      ),
      t('@yourbrand', 110, 1200, 860, 'Space Grotesk', 36, '#6366f1', { bold: true, align: 'left' }),
    ]),
    d('Testimonial', 1080, 1080, color('#ecfdf5'), [
      ...[0, 1, 2, 3, 4].map((i) => s('star', 330 + i * 86, 170, 76, 76, '#f59e0b')),
      t(
        '“The easiest tool we have used all year. Our team posts twice as often now.”',
        120,
        330,
        840,
        'Inter',
        60,
        '#064e3b',
        { lineHeight: 1.35 },
      ),
      s('ellipse', 500, 760, 80, 80, '#10b981'),
      t('Jane Doe, Example Co.', 140, 870, 800, 'Inter', 36, '#047857', { bold: true }),
    ]),
    d('YouTube thumbnail', 1280, 720, grad('#7c3aed', '#db2777', 0), [
      t('HOW I DID IT', 60, 180, 800, 'Anton', 170, '#ffffff', {
        align: 'left',
        stroke: { color: '#111827', width: 10 },
        lineHeight: 1,
      }),
      t('in 30 days', 66, 520, 700, 'Montserrat', 64, '#fde047', { bold: true, align: 'left' }),
      s('arrow', 860, 340, 300, 60, '#fde047', { strokeWidth: 26, rotation: -20 }),
    ]),
    d('Minimal', 1080, 1080, color('#fafaf9'), [
      t('Monday\nmotivation', 120, 330, 840, 'Caveat', 170, '#1c1917', { bold: true, lineHeight: 0.95 }),
      s('line', 390, 720, 300, 20, '#a8a29e', { strokeWidth: 4 }),
      t('Start small. Start today.', 140, 780, 800, 'DM Serif Display', 48, '#57534e'),
    ]),
    d('Launch', 1080, 1350, { kind: 'gradient', from: '#0f172a', via: '#4c1d95', to: '#db2777', angle: 120 }, [
      s('ellipse', 640, -120, 560, 560, '#f472b6', { opacity: 0.35 }),
      t('NEW', 100, 260, 300, 'Space Grotesk', 44, '#0f172a', {
        bold: true,
        letterSpacing: 6,
        box: { color: '#f9a8d4', padding: 18, radius: 40 },
      }),
      t('Meet the\nnew app', 100, 420, 880, 'Space Grotesk', 150, '#ffffff', {
        bold: true,
        align: 'left',
        lineHeight: 1,
      }),
      t('Faster, simpler, and free to try.', 100, 780, 880, 'Inter', 52, '#e9d5ff', { align: 'left' }),
      s('rect', 100, 1100, 420, 110, '#ffffff', { radius: 55 }),
      t('Try it today', 100, 1128, 420, 'Inter', 46, '#4c1d95', { bold: true }),
    ]),
    d('Big number', 1080, 1080, color('#f0fdf4'), [
      t('3x', 60, 180, 960, 'Anton', 420, '#16a34a'),
      t('more replies after we\nstarted posting daily', 120, 690, 840, 'Inter', 60, '#14532d', {
        bold: true,
        lineHeight: 1.3,
      }),
      t('Our 2026 numbers', 120, 930, 840, 'Inter', 34, '#4d7c0f', { letterSpacing: 2 }),
    ]),
    d('We are hiring', 1080, 1080, color('#111827'), [
      s('rect', 70, 70, 940, 940, '#111827', { stroke: '#facc15', strokeWidth: 6, radius: 40 }),
      t('WE ARE', 100, 230, 880, 'Bebas Neue', 150, '#ffffff', { letterSpacing: 8 }),
      t('HIRING', 100, 370, 880, 'Bebas Neue', 260, '#facc15', { letterSpacing: 8 }),
      t('Product designer · Remote', 100, 690, 880, 'Inter', 50, '#e5e7eb'),
      t('Apply at example.com/jobs', 100, 840, 880, 'Inter', 38, '#9ca3af'),
    ]),
    d('LinkedIn post', 1200, 627, grad('#0c4a6e', '#0369a1', 90), [
      t('How we cut our\nrelease time in half', 80, 150, 760, 'Montserrat', 66, '#ffffff', {
        bold: true,
        align: 'left',
        lineHeight: 1.15,
      }),
      t('A short case study', 80, 440, 760, 'Inter', 34, '#bae6fd', { align: 'left' }),
      s('ellipse', 880, 160, 300, 300, '#38bdf8', { fade: { side: 'left', size: 0.8 } }),
    ]),
    d('Story question', 1080, 1920, grad('#fde68a', '#fb7185', 160), [
      t('Quick question', 90, 560, 900, 'Poppins', 64, '#7c2d12', { bold: true }),
      s('rect', 90, 700, 900, 420, '#ffffff', {
        radius: 48,
        shadow: { color: '#000000', blur: 40, x: 0, y: 16, opacity: 0.15 },
      }),
      t('Coffee or tea\nto start the day?', 140, 820, 800, 'Poppins', 76, '#111827', { bold: true, lineHeight: 1.2 }),
      t('Answer in the comments', 90, 1220, 900, 'Inter', 44, '#7c2d12'),
    ]),
    carousel('Carousel', 1080, 1350, [
      [
        grad('#111827', '#1f2937', 90),
        [
          t('3 habits of\nteams that\nship', 100, 380, 880, 'Space Grotesk', 120, '#ffffff', {
            bold: true,
            align: 'left',
            lineHeight: 1.05,
          }),
          t('Swipe \u2192', 100, 1180, 880, 'Inter', 40, '#86efac', { bold: true, align: 'left' }),
        ],
      ],
      ...(
        [
          ['01', 'Write it down', 'Decisions live in docs, not in meetings.'],
          ['02', 'Ship small', 'Many small releases beat one big one.'],
          ['03', 'Look back', 'Fifteen minutes every Friday: what worked?'],
        ] as const
      ).map(([n, title, body]): [Fill, El[]] => [
        color('#f9fafb'),
        [
          t(n, 100, 200, 880, 'Anton', 220, '#16a34a', { align: 'left' }),
          t(title, 100, 520, 880, 'Space Grotesk', 96, '#111827', { bold: true, align: 'left' }),
          t(body, 100, 680, 880, 'Inter', 52, '#4b5563', { align: 'left', lineHeight: 1.4 }),
        ],
      ]),
    ]),
  ]
}
