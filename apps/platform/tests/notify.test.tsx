import { act, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/api'
import { describePlanLimit, planName, setNavigator, toastApiError } from '@/lib/notify'

import { renderWithProviders } from './utils'

const GB = 1024 ** 3

function planLimit(details?: Record<string, unknown>, message = 'plan limit reached') {
  return new ApiError(402, { code: 'plan_limit_exceeded', message, details })
}

/** Shows the toast for `err` and answers the one headed `heading`, the way a person reads it. */
async function toasted(heading: string, err: unknown, title?: string) {
  renderWithProviders(<p>page</p>)
  act(() => toastApiError(err, title))

  return (await screen.findByText(heading)).closest('li')!
}

const say = (details: Record<string, unknown>) => describePlanLimit(details, 'fallback')
const down = () => new ApiError(0, undefined, 'Cannot reach the staqly API. Is the server running?')

afterEach(async () => {
  setNavigator(null)
  act(() => void toast.dismiss())
  // Sonner takes a dismissed toast off ~200 ms later on a timer of its own: let it run while the page still exists,
  // or after the file's last test it fires into a torn-down jsdom ("window is not defined", an unhandled error).
  await act(() => new Promise((done) => setTimeout(done, 300)))
})

describe('a plan limit in plain words', () => {
  it('names the plan, what it includes and how much is used', () => {
    expect(say({ limit: 'channels', max: 3, used: 3, plan: 'starter' })).toBe(
      'Your Starter plan includes 3 channels (3 used).',
    )
    expect(say({ limit: 'posts_per_month', max: 1500, used: 1500, plan: 'plus' })).toBe(
      'Your Plus plan includes 1,500 posts per month (1,500 used).',
    )
    expect(say({ limit: 'api_keys', max: 2, plan: 'pro' })).toBe('Your Pro plan includes 2 API keys.')
    // A limit this dashboard has no words for still reads as words, and a plan it does not know by its name.
    expect(say({ limit: 'saved_replies', max: 5, used: 5, plan: 'agency' })).toBe(
      'Your Agency plan includes 5 saved replies (5 used).',
    )
    expect(say({ limit: 'webhooks', max: 0, used: 0 })).toBe('Your plan includes 0 webhooks (0 used).')
  })

  it('storage and upload size read in bytes people know', () => {
    expect(describePlanLimit({ limit: 'storage_bytes', max: 5 * GB, used: 4.5 * GB, plan: 'starter' }, 'x')).toBe(
      'Your Starter plan includes 5 GB of storage (4.5 GB used).',
    )
    expect(describePlanLimit({ limit: 'max_upload_bytes', max: 25 * 1024 ** 2, plan: 'starter' }, 'x')).toBe(
      'Your Starter plan accepts files up to 25 MB.',
    )
  })

  it("falls back to the server's message when it sent no numbers", () => {
    expect(describePlanLimit(undefined, 'the X posts of this month are used up')).toBe(
      'the X posts of this month are used up',
    )
    expect(describePlanLimit({ limit: 'channels' }, 'no numbers')).toBe('no numbers')
  })

  it('a local install is the Community plan', () => {
    expect(planName('unlocked')).toBe('Community')
    expect(planName('business')).toBe('Business')
    expect(planName()).toBe('')
  })
})

describe('the toast for a failed request', () => {
  it('a plan limit (402) explains the limit and leads to the plans', async () => {
    const go = vi.fn<(href: string) => void>()

    setNavigator(go)

    const shown = await toasted(
      'Plan limit reached',
      planLimit({ limit: 'channels', max: 3, used: 3, plan: 'starter' }),
    )

    expect(shown).toHaveTextContent('Your Starter plan includes 3 channels (3 used).')
    await userEvent.setup().click(within(shown).getByRole('button', { name: 'View plans' }))
    expect(go).toHaveBeenCalledWith('/settings/plan')
  })

  it("a 402 without numbers (an X allowance used up) shows the server's reason as a sentence", async () => {
    const shown = await toasted(
      'Plan limit reached',
      new ApiError(402, { code: 'x_posts_exhausted', message: 'the X posts of this month are used up' }),
    )

    expect(shown).toHaveTextContent('The X posts of this month are used up')
  })

  it('a workspace whose plan ended is called read-only, with the way to its plan', async () => {
    const go = vi.fn<(href: string) => void>()

    setNavigator(go)

    const shown = await toasted(
      'This workspace is read-only',
      new ApiError(402, { code: 'plan_expired', message: 'the plan of this workspace has ended' }),
    )

    expect(shown).toHaveTextContent('The plan of this workspace has ended')
    expect(screen.queryByText('Plan limit reached')).not.toBeInTheDocument()
    await userEvent.setup().click(within(shown).getByRole('button', { name: 'View plan' }))
    expect(go).toHaveBeenCalledWith('/settings/plan')
  })

  it('a rate limit (429) is calm and says when to try again', async () => {
    const shown = await toasted(
      'Slow down a little',
      new ApiError(429, { code: 'rate_limited', message: 'too many requests' }, undefined, 30),
    )

    expect(shown).toHaveTextContent('Too many requests. Try again in 30s.')
  })

  it("anything else shows the server's message, under the caller's title when it has one", async () => {
    const forbidden = new ApiError(403, { code: 'forbidden', message: 'missing scope channels:manage' })

    expect(await toasted('Missing scope channels:manage', forbidden)).toBeInTheDocument()

    const titled = await toasted('Could not add the channel', forbidden, 'Could not add the channel')

    expect(titled).toHaveTextContent('Missing scope channels:manage')
  })

  it('the same failure of several requests at once shows once', async () => {
    renderWithProviders(<p>page</p>)
    act(() => {
      toastApiError(down())
      toastApiError(down())
      toastApiError(down())
    })

    expect(await screen.findAllByText('Cannot reach the staqly API. Is the server running?')).toHaveLength(1)
  })
})
