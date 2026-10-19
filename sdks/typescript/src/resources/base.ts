import type { HTTPClient } from '../core.js'
import type { Page } from '../types.js'

/** Shared plumbing for resource namespaces. */
export abstract class APIResource {
  constructor(protected readonly http: HTTPClient) {}
}

export const enc = encodeURIComponent

/** Walks every page of a cursor-paginated endpoint. */
export async function* paginate<T, P extends { cursor?: string }>(
  fetchPage: (params: P) => Promise<Page<T>>,
  params: P,
): AsyncGenerator<T, void, undefined> {
  let cursor = params.cursor

  do {
    const page = await fetchPage({ ...params, cursor })

    yield* page.data
    cursor = page.next_cursor || undefined
  } while (cursor)
}
