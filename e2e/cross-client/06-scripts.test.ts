// The repository's shell scripts and the n8n demo workflow, run as they are:
//   scripts/seed.sh    against the local server (curl and the public API);
//   scripts/restore.sh with a stub `docker` on throwaway folders, never a real volume: what a damaged
//                      backup or a failed swap leaves behind;
//   the demo workflow's "Verify & parse" node, with n8n's helpers stubbed.
import { execFileSync } from 'node:child_process'
import { createHmac, randomBytes } from 'node:crypto'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'

import { describe, expect, inject, it } from 'vitest'

import { ROOT, run, sdk } from './lib.js'

const local = inject('local')
const seed = (key: string) =>
  run('bash', [join(ROOT, 'scripts/seed.sh')], {
    env: { ...process.env, STAQLY_API_URL: local.url, STAQLY_API_KEY: key },
  })

describe('scripts/seed.sh', () => {
  it('creates the demo collections, pictures and posts, twice in a row', async () => {
    const staqly = sdk(local.url, local.token)
    const first = await seed(local.token)

    expect(first).toMatchObject({ code: 0, err: '' })
    expect(first.out).not.toMatch(/^[45]\d\d /m)
    expect(first.out + first.err).not.toContain(local.token)

    const avatars = (await staqly.posts.list({ collection_id: 'Instagram Avatar' })).data

    expect(avatars.map((p) => p.title).toSorted()).toEqual([1, 2, 3, 4].map((n) => `Avatar post #${n}`))
    expect(avatars.every((p) => p.media.length === 1 && p.status === 'draft')).toBe(true)
    expect((await staqly.posts.list({ collection_id: 'AI Research' })).data).toHaveLength(2)
    expect((await staqly.posts.list({ collection_id: 'Automation Results' })).data).toHaveLength(1)

    // The collections are already there (409): the second run still ends well.
    const second = await seed(local.token)

    expect(second).toMatchObject({ code: 0, err: '' })
    expect(second.out).toContain('409 collection Instagram Avatar')
  })

  it('stops with exit 1 at the first request the API refuses', async () => {
    const r = await seed('stk_local_wrong')

    expect(r.code).toBe(1)
    expect(r.out.trim()).toBe('401 collection Instagram Avatar')
    expect(r.err).toContain('unauthorized')
  })
})

describe('scripts/restore.sh (stub docker, throwaway folders)', () => {
  // The stub runs the script's inner shell on folders instead of a volume: /data and /backup become
  // $STUB_ROOT/data and $STUB_ROOT/backup, chown is dropped, and STUB_FAIL_AFTER makes the swap fail
  // right after that file was moved into place (STUB_KILL_AFTER kills the helper there instead: no trap runs).
  const STUB = `#!/bin/sh
echo "$1" >> "$STUB_ROOT/calls.log"
case "$1" in
  stop|start) exit 0 ;;
  run)
    for last; do :; done
    script=$(printf '%s' "$last" | sed "s#/data#$STUB_ROOT/data#g; s#/backup#$STUB_ROOT/backup#g; s#chown -R 10001:10001#: #")
    if [ -n "\${STUB_KILL_AFTER:-}" ]; then
      script=$(printf '%s' "$script" | sed "s#installed=\\"\\$installed \\$name\\"#&; [ \\"\\$name\\" != \\"$STUB_KILL_AFTER\\" ] || kill -9 \\$\\$#")
    fi
    if [ -n "\${STUB_FAIL_AFTER:-}" ]; then
      script=$(printf '%s' "$script" | sed "s#installed=\\"\\$installed \\$name\\"#&; [ \\"\\$name\\" != \\"$STUB_FAIL_AFTER\\" ] || exit 9#")
    fi
    exec sh -ec "$script" ;;
  *) echo "stub: unexpected docker $*" >&2; exit 97 ;;
esac
`

  function install() {
    const root = mkdtempSync(join(tmpdir(), 'staqly-xc-restore-'))
    const at = (...p: string[]) => join(root, ...p)

    for (const d of ['bin', 'data/uploads', 'backup', 'archive/uploads']) {
      mkdirSync(at(d), { recursive: true })
    }

    writeFileSync(at('bin/docker'), STUB)
    chmodSync(at('bin/docker'), 0o755)
    writeFileSync(at('data/workspace.db'), 'CURRENT DATABASE')
    writeFileSync(at('data/workspace.db-wal'), 'current wal')
    writeFileSync(at('data/uploads/current.png'), 'current picture')
    writeFileSync(at('data/secret.key'), 'current key')
    writeFileSync(at('data/access_token'), 'current token')
    // A real database: the script refuses a backup whose workspace.db is not one.
    const backupDb = new DatabaseSync(at('backup/workspace.db'))

    backupDb.exec("CREATE TABLE marker (value TEXT); INSERT INTO marker VALUES ('BACKUP DATABASE')")
    backupDb.close()
    writeFileSync(at('archive/uploads/old.png'), randomBytes(200_000)) // does not compress: truncating cuts into it
    writeFileSync(at('archive/secret.key'), 'backup key')
    execFileSync('tar', ['czf', at('backup/uploads.tar.gz'), '-C', at('archive'), 'uploads', 'secret.key'])

    const read = (p: string) => readFileSync(at(p), 'utf8')
    const calls = () => (existsSync(at('calls.log')) ? read('calls.log').trim().split('\n') : [])
    const restore = (env: Record<string, string> = {}) =>
      run('bash', [join(ROOT, 'scripts/restore.sh'), at('backup')], {
        env: { ...process.env, ...env, STUB_ROOT: root, PATH: `${at('bin')}:${process.env.PATH}`, FORCE: '1' },
      })
    const untouched = () => {
      expect(read('data/workspace.db')).toBe('CURRENT DATABASE')
      expect(read('data/workspace.db-wal')).toBe('current wal')
      expect(read('data/uploads/current.png')).toBe('current picture')
      expect(read('data/secret.key')).toBe('current key')
      expect(existsSync(at('data/uploads/old.png'))).toBe(false)
      expect(existsSync(at('data/.restore-new'))).toBe(false)
    }

    return { at, read, calls, restore, untouched }
  }

  it('restores the database, uploads and keys, and starts the container again', async () => {
    const t = install()
    const r = await t.restore()

    expect(r).toMatchObject({ code: 0, err: '' })
    expect(t.calls()).toEqual(['stop', 'run', 'start'])
    expect(readFileSync(t.at('data/workspace.db')).equals(readFileSync(t.at('backup/workspace.db')))).toBe(true)
    expect(existsSync(t.at('data/workspace.db-wal'))).toBe(false) // never replayed onto the restored database
    expect(existsSync(t.at('data/uploads/old.png'))).toBe(true)
    expect(existsSync(t.at('data/uploads/current.png'))).toBe(false)
    expect(t.read('data/secret.key')).toBe('backup key')
    expect(t.read('data/access_token')).toBe('current token') // not in the backup: kept
    expect(statSync(t.at('data/workspace.db')).mode & 0o777).toBe(0o600)
    expect(existsSync(t.at('data/.restore-new')) || existsSync(t.at('data/.restore-old'))).toBe(false)
  })

  it('refuses a truncated uploads archive before it stops or changes anything', async () => {
    const t = install()

    writeFileSync(t.at('backup/uploads.tar.gz'), readFileSync(t.at('backup/uploads.tar.gz')).subarray(0, 5000))
    const r = await t.restore()

    expect(r.code).toBe(1)
    expect(r.err).toContain('nothing was changed')
    expect(t.calls()).toEqual([])
    t.untouched()
  })

  it('leaves the current data alone when the backup cannot be unpacked in the volume', async () => {
    const t = install()

    // The backup passes the checks on the host, but the helper cannot stage it: the volume takes no new folder.
    chmodSync(t.at('data'), 0o555)
    const r = await t.restore()

    chmodSync(t.at('data'), 0o755)

    expect(r.code).toBe(1)
    expect(r.err).toContain('previous data is unchanged')
    expect(t.calls()).toEqual(['stop', 'run', 'run', 'start'])
    t.untouched()
  })

  it('puts the previous database, uploads and keys back when the swap fails half way', async () => {
    const t = install()
    const r = await t.restore({ STUB_FAIL_AFTER: 'uploads' })

    expect(r.code).toBe(1)
    expect(r.err).toContain('previous data is unchanged')
    expect(t.calls()).toEqual(['stop', 'run', 'run', 'start'])
    t.untouched()
    expect(existsSync(t.at('data/.restore-old'))).toBe(false)
  })

  it('refuses a database that is cut off or not SQLite before it stops or changes anything', async () => {
    for (const damaged of [readFileSync(install().at('backup/workspace.db')).subarray(0, 2000), 'BACKUP DATABASE']) {
      const t = install()

      writeFileSync(t.at('backup/workspace.db'), damaged)
      const r = await t.restore()

      expect(r.code).toBe(1)
      expect(r.err).toContain('nothing was changed')
      expect(t.calls()).toEqual([])
      t.untouched()
    }
  })

  it('leaves the container stopped when the helper was killed in the middle of the swap', async () => {
    const t = install()
    const r = await t.restore({ STUB_KILL_AFTER: 'workspace.db' })

    expect(r.code).toBe(1)
    expect(r.err).toMatch(/left STOPPED/)
    expect(t.calls()).toEqual(['stop', 'run', 'run'])
    expect(t.read('data/.restore-old/workspace.db')).toBe('CURRENT DATABASE')
  })

  it('says so when it leaves the container stopped: an earlier restore left data aside', async () => {
    const t = install()

    mkdirSync(t.at('data/.restore-old'))
    writeFileSync(t.at('data/.restore-old/workspace.db'), 'ONLY COPY')
    const r = await t.restore()

    expect(r.code).toBe(1)
    expect(r.err).toMatch(/left STOPPED/)
    expect(t.calls()).toEqual(['stop', 'run'])
    expect(t.read('data/.restore-old/workspace.db')).toBe('ONLY COPY')
    t.untouched()
  })
})

describe('n8n demo workflow: Verify & parse', () => {
  const secret = 'whsec_test'
  const workflow = JSON.parse(readFileSync(join(ROOT, 'integrations/n8n/instagram-avatar-demo.json'), 'utf8')) as {
    nodes: { name: string; parameters: { jsCode: string } }[]
  }
  const code = workflow.nodes
    .find((n) => n.name === 'Verify & parse')!
    .parameters.jsCode.replace("webhookSecret: '',", `webhookSecret: '${secret}',`)
  // The node's own code, as n8n's Code node runs it.
  const AsyncFunction = Object.getPrototypeOf(async () => {}).constructor as new (
    ...args: string[]
  ) => (...args: unknown[]) => Promise<unknown[]>
  const node = new AsyncFunction('$input', '$getWorkflowStaticData', 'require', code)
  const state = {}
  const verify = (items: unknown[]) =>
    node.call({ helpers: {} }, { all: () => items }, () => state, createRequire(import.meta.url))

  function delivery(eventId: string, header = eventId) {
    const body = { id: eventId, type: 'post.publish_requested', data: { post: { id: 'pst_1' } } }
    const ts = String(Math.floor(Date.now() / 1000))
    const signature = `sha256=${createHmac('sha256', secret).update(`${ts}.`).update(JSON.stringify(body)).digest('hex')}`

    return {
      json: {
        body,
        headers: { 'x-staqly-timestamp': ts, 'x-staqly-signature': signature, 'x-staqly-event-id': header },
      },
    }
  }

  it('drops a repeated event and refuses an event id header that differs from the signed body', async () => {
    expect(await verify([delivery('evt_1')])).toHaveLength(1)
    expect(await verify([delivery('evt_1')])).toHaveLength(0)
    // The header is not signed: a captured delivery replayed with another id must not run the publisher again.
    await expect(verify([delivery('evt_1', 'evt_other')])).rejects.toThrow(/does not match the signed event id/)
    expect(await verify([delivery('evt_2')])).toHaveLength(1)
  })
})
