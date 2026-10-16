// Cloud signup creates an isolated workspace on a Pro trial; the Free plan
// allows unlimited items but 50 approvals a month. Needs the cloud-mode stack
// that 07-cloud-mode leaves behind.
import { type Item, PlanLimitError, type WorkspaceCreated } from '@staqly/sdk'
import { beforeAll, describe, expect, it } from 'vitest'

import { sdk } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { API, server, SETUP_TOKEN } from '../lib/stack.js'

describe('cloud signup and plans', () => {
  let signup: WorkspaceCreated

  beforeAll(async () => {
    if ((await sdk().setup()).mode !== 'cloud') {
      throw new Error('08-signup-and-plans needs the cloud-mode stack from 07-cloud-mode')
    }
  })

  it('refuses signup without the SETUP_TOKEN', async () => {
    const res = await http(`${API}/v1/workspaces`, { method: 'POST', json: { name: 'Nope' } })

    expect(res.status).toBe(403)
  })

  it('signs up a new workspace on a Pro trial', async () => {
    signup = await sdk().workspaces.create({ name: 'Second', setup_token: SETUP_TOKEN })

    expect(signup.workspace.plan).toBe('pro')
    expect(signup.api_key.key).toMatch(/^stk_/)
  })

  it('the new workspace sees no other data', async () => {
    expect((await sdk(signup.api_key.key).items.list()).data).toHaveLength(0)
  })

  it('reports the trial in usage', async () => {
    const usage = await sdk(signup.api_key.key).workspace.usage()

    expect(usage.plan.id).toBe('pro')
    expect(usage.plan_expires_at).not.toBeNull()
    expect(usage.suspended).toBe(false)
  })

  it('on Free: unlimited items, 50 approvals a month, then 402 until the operator upgrades', async () => {
    // 100+ requests in a row hit the rate limit; the SDK waits out each 429.
    const owner = sdk(signup.api_key.key, 10)

    await server('set-plan', signup.workspace.id, 'free')
    await owner.collections.create({ name: 'Q' })
    const items: Item[] = []

    for (let i = 1; i <= 51; i++) {
      items.push(await owner.items.create('Q', { title: String(i) }))
    }

    for (const item of items.slice(0, 50)) {
      await owner.items.approve(item.id)
    }

    const last = items[50]!
    const err = await owner.items.approve(last.id).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(PlanLimitError)
    expect((err as PlanLimitError).status).toBe(402)
    expect((err as PlanLimitError).limit).toBe('decisions_per_month')

    await server('set-plan', signup.workspace.id, 'pro')
    expect((await owner.items.approve(last.id)).status).toBe('approved')
  })
})
