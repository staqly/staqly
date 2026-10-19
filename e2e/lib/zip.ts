// Minimal ZIP reader for the export tests: the central directory and stored
// or deflated entries (all the workspace export writes).
import { inflateRawSync } from 'node:zlib'

export function unzip(buf: Buffer): Map<string, Buffer> {
  const eocd = buf.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]))

  if (eocd < 0) {
    throw new Error('not a ZIP file: no end of central directory')
  }

  const count = buf.readUInt16LE(eocd + 10)
  let at = buf.readUInt32LE(eocd + 16)
  const out = new Map<string, Buffer>()

  for (let i = 0; i < count; i++) {
    if (buf.readUInt32LE(at) !== 0x02014b50) {
      throw new Error(`bad central directory entry ${i}`)
    }

    const method = buf.readUInt16LE(at + 10)
    const compressed = buf.readUInt32LE(at + 20)
    const nameLen = buf.readUInt16LE(at + 28)
    const extraLen = buf.readUInt16LE(at + 30)
    const commentLen = buf.readUInt16LE(at + 32)
    const local = buf.readUInt32LE(at + 42)
    const name = buf.toString('utf8', at + 46, at + 46 + nameLen)
    const dataAt = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28)
    const data = buf.subarray(dataAt, dataAt + compressed)

    out.set(name, method === 8 ? inflateRawSync(data) : Buffer.from(data))
    at += 46 + nameLen + extraLen + commentLen
  }

  return out
}

/** Parses an NDJSON file of the archive. */
export function ndjson<T = Record<string, unknown>>(files: Map<string, Buffer>, name: string): T[] {
  const text = files.get(name)?.toString('utf8') ?? ''

  return text
    .split('\n')
    .filter((line) => line.trim() !== '')
    .map((line) => JSON.parse(line) as T)
}
