// Node/Bun/Deno helpers that need the file system. Kept out of the main entry
// so edge bundles never see `node:` imports.
import { createWriteStream } from 'node:fs'
import { mkdtemp, readFile, rename, rm } from 'node:fs/promises'
import { basename, dirname, join } from 'node:path'
import { Readable } from 'node:stream'
import { pipeline } from 'node:stream/promises'
import type { ReadableStream as NodeReadableStream } from 'node:stream/web'

/** Reads a file into a `File` for `media.upload`. */
export async function fileFromPath(path: string, options: { filename?: string; type?: string } = {}): Promise<File> {
  const bytes = await readFile(path)

  return new File(
    [new Uint8Array(bytes)],
    options.filename ?? basename(path),
    options.type ? { type: options.type } : {},
  )
}

/**
 * Streams a `Response` body (e.g. `exports.workspaceExportResponse(id)` or
 * `exports.eventsResponse({format: "csv"})`) to a file without buffering it.
 * Returns the number of bytes written.
 */
export async function saveResponse(res: Response, path: string): Promise<number> {
  if (!res.body) {
    throw new Error('response has no body')
  }

  let temporary: string | undefined

  try {
    temporary = await mkdtemp(join(dirname(path), '.staqly-download-'))

    const file = join(temporary, 'download')
    const out = createWriteStream(file, { mode: 0o600 })

    await pipeline(Readable.fromWeb(res.body as unknown as NodeReadableStream), out)
    await rename(file, path)

    return out.bytesWritten
  } finally {
    if (temporary) {
      await rm(temporary, { recursive: true, force: true })
    } else {
      await res.body.cancel().catch(() => undefined)
    }
  }
}
