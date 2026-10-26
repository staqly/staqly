// A ZIP archive of files stored as they are (no compression: JPEGs don't shrink anyway), so the image tools can hand
// over every piece in one download without a library. No imports, so scripts/check-tools.mjs can run it with Node.

export type ZipEntry = { name: string; data: Uint8Array }

let table: Uint32Array | undefined

/** CRC-32 (the ZIP and PNG checksum) of `data`. */
export function crc32(data: Uint8Array) {
  if (!table) {
    table = new Uint32Array(256)

    for (let n = 0; n < 256; n++) {
      let c = n

      for (let k = 0; k < 8; k++) {
        c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
      }

      table[n] = c >>> 0
    }
  }

  let crc = 0xffffffff

  for (const byte of data) {
    crc = table[(crc ^ byte) & 0xff]! ^ (crc >>> 8)
  }

  return (crc ^ 0xffffffff) >>> 0
}

/** The date and time in the two 16-bit MS-DOS fields ZIP uses (local time, two-second steps). */
function dosTime(d: Date) {
  return {
    time: (d.getHours() << 11) | (d.getMinutes() << 5) | Math.floor(d.getSeconds() / 2),
    date: ((Math.max(1980, d.getFullYear()) - 1980) << 9) | ((d.getMonth() + 1) << 5) | d.getDate(),
  }
}

/** A ZIP archive holding `entries`, uncompressed. Names are UTF-8; each file stays under 4 GB. */
export function zip(entries: ZipEntry[], at = new Date()): Uint8Array {
  const enc = new TextEncoder()
  const { time, date } = dosTime(at)
  const locals: Uint8Array[] = []
  const centrals: Uint8Array[] = []
  let offset = 0

  for (const e of entries) {
    const name = enc.encode(e.name)
    const crc = crc32(e.data)
    const local = new Uint8Array(30 + name.length)
    const lv = new DataView(local.buffer)

    lv.setUint32(0, 0x04034b50, true) // local file header
    lv.setUint16(4, 20, true) // version needed: 2.0
    lv.setUint16(6, 0x0800, true) // UTF-8 names
    lv.setUint16(8, 0, true) // stored
    lv.setUint16(10, time, true)
    lv.setUint16(12, date, true)
    lv.setUint32(14, crc, true)
    lv.setUint32(18, e.data.length, true)
    lv.setUint32(22, e.data.length, true)
    lv.setUint16(26, name.length, true)
    local.set(name, 30)

    const central = new Uint8Array(46 + name.length)
    const cv = new DataView(central.buffer)

    cv.setUint32(0, 0x02014b50, true) // central directory header
    cv.setUint16(4, 20, true) // made by
    cv.setUint16(6, 20, true)
    cv.setUint16(8, 0x0800, true)
    cv.setUint16(10, 0, true)
    cv.setUint16(12, time, true)
    cv.setUint16(14, date, true)
    cv.setUint32(16, crc, true)
    cv.setUint32(20, e.data.length, true)
    cv.setUint32(24, e.data.length, true)
    cv.setUint16(28, name.length, true)
    cv.setUint32(42, offset, true)
    central.set(name, 46)

    locals.push(local, e.data)
    centrals.push(central)
    offset += local.length + e.data.length
  }

  const centralSize = centrals.reduce((n, c) => n + c.length, 0)
  const end = new Uint8Array(22)
  const ev = new DataView(end.buffer)

  ev.setUint32(0, 0x06054b50, true) // end of central directory
  ev.setUint16(8, entries.length, true)
  ev.setUint16(10, entries.length, true)
  ev.setUint32(12, centralSize, true)
  ev.setUint32(16, offset, true)

  const parts = [...locals, ...centrals, end]
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let pos = 0

  for (const p of parts) {
    out.set(p, pos)
    pos += p.length
  }

  return out
}
