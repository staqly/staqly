// A PDF of pictures, one per page at the picture's size: what a carousel becomes for LinkedIn's document posts.
// ponytail: JPEG pages only (PDF takes them as they are, DCTDecode); a PDF library if text or vectors are needed.

const enc = new TextEncoder()

/** The pixel size of a JPEG, from its SOF marker. */
export function jpegSize(b: Uint8Array): { width: number; height: number } {
  let i = 2

  while (i < b.length) {
    const marker = b[i + 1]!
    const len = (b[i + 2]! << 8) | b[i + 3]!

    // SOF0..SOF15, but not DHT (c4), JPG (c8) or DAC (cc).
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      return { height: (b[i + 5]! << 8) | b[i + 6]!, width: (b[i + 7]! << 8) | b[i + 8]! }
    }

    i += 2 + len
  }

  throw new Error('Not a JPEG')
}

/** One PDF page per JPEG, each the picture's pixel size in points. */
export function jpegsToPdf(jpegs: Uint8Array[]): Blob {
  const parts: (Uint8Array | string)[] = []
  const offsets: number[] = []
  let length = 0
  const push = (p: Uint8Array | string) => {
    parts.push(p)
    length += typeof p === 'string' ? enc.encode(p).length : p.length
  }
  const object = (n: number, body: string, stream?: Uint8Array) => {
    offsets[n] = length
    push(`${n} 0 obj\n${body}\n`)

    if (stream) {
      push('stream\n')
      push(stream)
      push('\nendstream\n')
    }

    push('endobj\n')
  }

  push('%PDF-1.4\n')

  // 1: catalog, 2: pages, then per page: page, picture, drawing.
  const kids = jpegs.map((_, i) => `${3 + i * 3} 0 R`).join(' ')

  object(1, '<< /Type /Catalog /Pages 2 0 R >>')
  object(2, `<< /Type /Pages /Kids [${kids}] /Count ${jpegs.length} >>`)

  for (const [i, jpg] of jpegs.entries()) {
    const n = 3 + i * 3
    const { width: w, height: h } = jpegSize(jpg)
    const draw = enc.encode(`q ${w} 0 0 ${h} 0 0 cm /P Do Q`)

    object(
      n,
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${w} ${h}] /Resources << /XObject << /P ${n + 1} 0 R >> >> /Contents ${n + 2} 0 R >>`,
    )
    object(
      n + 1,
      `<< /Type /XObject /Subtype /Image /Width ${w} /Height ${h} /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length ${jpg.length} >>`,
      jpg,
    )
    object(n + 2, `<< /Length ${draw.length} >>`, draw)
  }

  const count = 3 + jpegs.length * 3
  const xref = length

  push(`xref\n0 ${count}\n0000000000 65535 f \n`)

  for (let n = 1; n < count; n++) {
    push(`${String(offsets[n]).padStart(10, '0')} 00000 n \n`)
  }

  push(`trailer\n<< /Size ${count} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`)

  return new Blob(parts as BlobPart[], { type: 'application/pdf' })
}
