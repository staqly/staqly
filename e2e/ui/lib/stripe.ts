// A Stripe stand-in for the billing server (STRIPE_API_BASE): the few REST calls staqly makes, with state, and
// the two things only Stripe does: its hosted Checkout page (GET /pay/{session}: "the customer pays", the
// subscription exists, the events go out, the browser returns to success_url) and signed webhook events. Nothing
// is ever charged and no real Stripe is called. A call it does not know answers 404 and is listed in
// /_test/state `unknown`, which the spec expects to stay empty: a new Stripe call in the product must be added
// here on purpose.
// shortcut: no invoices, proration amounts, promotion codes or declined cards (the Go tests fake those per route:
// apps/api/internal/httpapi/billing_*_test.go); add them here when a journey in the browser needs one.
import { createHmac } from 'node:crypto'
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'

export const STRIPE_KEY = 'sk_test_ui_e2e'
export const STRIPE_WHSEC = 'whsec_ui_e2e'

type Obj = Record<string, unknown>

interface Item {
  id: string
  quantity: number
  current_period_start: number
  current_period_end: number
  price: Obj
}

interface Subscription extends Obj {
  id: string
  customer: string
  status: string
  items: { data: Item[] }
}

const now = () => Math.floor(Date.now() / 1000)
const list = (data: unknown[]) => ({ object: 'list', data, has_more: false })

// The plans' monthly prices in EUR, as in apps/api/internal/app/plans.go (the plan picker orders plans by them).
const MONTHLY: Record<string, number> = { starter: 15, plus: 29, pro: 59, business: 99 }

/** price_plus_year <-> staqly_plus_year: every lookup key staqly asks for exists (what is no plan costs 10 EUR). */
function price(idOrKey: string): Obj {
  const name = idOrKey.replace(/^(price_|staqly_)/, '')
  const interval = /_(month|year)$/.exec(name)?.[1]
  const monthly = MONTHLY[name.replace(/_(month|year)$/, '')] ?? 10

  return {
    id: `price_${name}`,
    lookup_key: `staqly_${name}`,
    unit_amount: monthly * (interval === 'year' ? 10 : 1) * 100,
    currency: 'eur',
    metadata: {},
    product: name.startsWith('addon_') ? 'prod_addon' : `prod_${name}`,
    recurring: interval ? { interval } : null,
  }
}

async function body(req: IncomingMessage): Promise<string> {
  const chunks: Buffer[] = []

  for await (const c of req) {
    chunks.push(c as Buffer)
  }

  return Buffer.concat(chunks).toString('utf8')
}

export interface FakeStripe {
  url: string
  close: () => Promise<void>
}

/** Starts the stand-in; `webhook` is staqly's POST /v1/billing/webhook. */
export async function startStripe(port: number, webhook: string): Promise<FakeStripe> {
  const url = `http://127.0.0.1:${port}`
  const customers = new Map<string, Obj>()
  const subs = new Map<string, Subscription>()
  const sessions = new Map<string, URLSearchParams>()
  const calls: string[] = []
  const unknown: string[] = []
  // Ids never repeat across starts: a server kept running (UI_REUSE=1) remembers the events it applied.
  const boot = Date.now().toString(36)
  let seq = 0
  const id = (prefix: string) => `${prefix}_${boot}${++seq}`
  /** As Stripe answers: the card and the latest invoice are ids unless the request expands them. */
  const shown = (sub: Subscription, expand: string[]) => ({
    ...sub,
    default_payment_method: expand.includes('default_payment_method')
      ? sub.default_payment_method
      : (sub.default_payment_method as Obj).id,
    latest_invoice: expand.includes('latest_invoice') ? sub.latest_invoice : (sub.latest_invoice as Obj).id,
  })

  /** Sends a signed event to staqly, as Stripe does; answers staqly's status. */
  async function emit(type: string, object: unknown, secret = STRIPE_WHSEC): Promise<number> {
    const payload = JSON.stringify({ id: id('evt'), type, data: { object } })
    const t = now()
    const v1 = createHmac('sha256', secret).update(`${t}.${payload}`).digest('hex')
    const res = await fetch(webhook, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Stripe-Signature': `t=${t},v1=${v1}` },
      body: payload,
    })

    return res.status
  }

  /** After staqly's own change: the event follows the response, as with Stripe. */
  const later = (type: string, sub: Subscription) =>
    void setTimeout(() => void emit(type, shown(sub, [])).catch(() => {}), 20)

  /** The Checkout page: the customer pays, Stripe tells staqly, the browser goes back. */
  async function pay(sessionId: string): Promise<string | undefined> {
    const form = sessions.get(sessionId)

    if (!form) {
      return undefined
    }

    const metadata = (prefix: string) =>
      Object.fromEntries(
        [...form].filter(([k]) => k.startsWith(`${prefix}[`)).map(([k, v]) => [k.slice(prefix.length + 1, -1), v]),
      )
    const session: Obj = {
      id: sessionId,
      mode: form.get('mode'),
      payment_status: 'paid',
      customer: form.get('customer'),
      client_reference_id: form.get('client_reference_id'),
      metadata: metadata('metadata'),
    }

    if (form.get('mode') === 'subscription') {
      const start = now()
      const trialDays = Number(form.get('subscription_data[trial_period_days]') ?? 0)
      const p = price(form.get('line_items[0][price]')!)
      const end =
        start + (trialDays ? trialDays : p.recurring && (p.recurring as Obj).interval === 'year' ? 365 : 30) * 86400
      const sub: Subscription = {
        id: id('sub'),
        customer: form.get('customer')!,
        status: trialDays ? 'trialing' : 'active',
        trial_end: trialDays ? end : null,
        current_period_start: start,
        current_period_end: end,
        metadata: metadata('subscription_data[metadata]'),
        cancellation_details: { reason: null },
        // Each customer pays with a card of their own (staqly remembers the cards of unpaid trials).
        default_payment_method: { id: id('pm'), card: { fingerprint: `fp_${form.get('customer')}`, last4: '4242' } },
        latest_invoice: { id: id('in'), status: 'paid', billing_reason: 'subscription_create' },
        items: {
          data: [
            {
              id: id('si'),
              quantity: Number(form.get('line_items[0][quantity]') ?? 1),
              current_period_start: start,
              current_period_end: end,
              price: p,
            },
          ],
        },
      }

      subs.set(sub.id, sub)
      session.subscription = sub.id
      await emit('customer.subscription.created', shown(sub, []))
    } else {
      session.payment_intent = id('pi')
    }

    await emit('checkout.session.completed', session)

    return form.get('success_url') ?? undefined
  }

  /** POST /v1/subscriptions/{id}: prices and quantities of items, a new item, the trial's end. */
  function update(sub: Subscription, form: URLSearchParams) {
    for (let i = 0; form.has(`items[${i}][id]`) || form.has(`items[${i}][price]`); i++) {
      const itemId = form.get(`items[${i}][id]`)
      const item = sub.items.data.find((it) => it.id === itemId)
      const p = form.get(`items[${i}][price]`)
      const q = form.get(`items[${i}][quantity]`)

      if (item) {
        item.price = p ? price(p) : item.price
        item.quantity = q ? Number(q) : item.quantity
      } else if (p) {
        sub.items.data.push({ ...sub.items.data[0]!, id: id('si'), quantity: Number(q ?? 1), price: price(p) })
      }
    }

    for (const [k, v] of form) {
      if (k.startsWith('metadata[')) {
        ;(sub.metadata as Obj)[k.slice(9, -1)] = v
      }
    }

    if (form.get('trial_end') === 'now') {
      sub.status = 'active'
      sub.trial_end = null
      sub.current_period_start = now()
      sub.current_period_end = now() + 30 * 86400

      for (const it of sub.items.data) {
        it.current_period_start = sub.current_period_start as number
        it.current_period_end = sub.current_period_end as number
      }
    }
  }

  /** The features Stripe grants a customer: one per add-on on a live subscription. */
  const addonsOf = (customer: string | null) =>
    [...subs.values()]
      .filter((s) => s.customer === customer && ['active', 'trialing', 'past_due'].includes(s.status))
      .flatMap((s) => s.items.data)
      .map((it) => /^staqly_addon_(.+)_(month|year)$/.exec(String(it.price.lookup_key))?.[1])
      .filter((a): a is string => !!a)

  async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const u = new URL(req.url ?? '/', url)
    const text = await body(req)
    const send = (v: unknown, status = 200) => {
      res.writeHead(status, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(v))
    }
    const route = `${req.method} ${u.pathname}`
    let m: RegExpExecArray | null

    // What the spec drives and reads (not Stripe's API).
    if (route === 'GET /_test/state') {
      return send({ calls, unknown, subscriptions: [...subs.values()], customers: [...customers.values()] })
    }

    if ((m = /^POST \/_test\/subscriptions\/([^/]+)$/.exec(route))) {
      // Stripe changed the subscription (a renewal failed, the retries ran out): merge and tell staqly.
      const sub = subs.get(m[1]!)
      const { patch, event } = JSON.parse(text) as { patch: Obj; event?: string }

      if (!sub) {
        return send({ error: 'no such subscription' }, 404)
      }

      Object.assign(sub, patch)

      return send({ status: await emit(event ?? 'customer.subscription.updated', shown(sub, [])) })
    }

    if (route === 'POST /_test/event') {
      const e = JSON.parse(text) as { type: string; object: unknown; secret?: string }

      return send({ status: await emit(e.type, e.object, e.secret) })
    }

    if ((m = /^GET \/pay\/([^/]+)$/.exec(route))) {
      const back = await pay(m[1]!)

      res.writeHead(back ? 303 : 404, back ? { Location: back } : {})

      res.end()

      return
    }

    if (route === 'GET /portal') {
      res.writeHead(200, { 'Content-Type': 'text/html' })

      res.end('<!doctype html><title>Stripe portal (stand-in)</title><h1>Billing portal</h1>')

      return
    }

    // Stripe's API: the secret key as the basic-auth user.
    const key = Buffer.from((req.headers.authorization ?? '').replace(/^Basic /, ''), 'base64').toString()

    if (key !== `${STRIPE_KEY}:`) {
      return send({ error: { type: 'invalid_request_error', message: 'Invalid API Key provided' } }, 401)
    }

    const form = new URLSearchParams(text)
    const expand = [...u.searchParams.getAll('expand[]'), ...form.getAll('expand[]')]

    calls.push(route)

    if (route === 'GET /v1/prices') {
      return send(list([price(u.searchParams.get('lookup_keys[]')!)]))
    }

    if ((m = /^GET \/v1\/prices\/([^/]+)$/.exec(route))) {
      return send(price(m[1]!))
    }

    if (route === 'POST /v1/customers') {
      const c = { id: id('cus'), ...Object.fromEntries(form) }

      customers.set(c.id, c)

      return send(c)
    }

    if ((m = /^(GET|POST) \/v1\/customers\/([^/]+)$/.exec(route))) {
      const c = customers.get(m[2]!)

      if (c && req.method === 'POST') {
        Object.assign(c, Object.fromEntries(form))
      }

      return c ? send(c) : send({ error: { code: 'resource_missing', message: 'No such customer' } }, 404)
    }

    if (/^GET \/v1\/customers\/[^/]+\/payment_methods$/.test(route)) {
      return send(list([]))
    }

    if (route === 'POST /v1/checkout/sessions') {
      const cs = id('cs')

      sessions.set(cs, form)

      return send({ id: cs, url: `${url}/pay/${cs}` })
    }

    if (route === 'GET /v1/checkout/sessions' || route === 'GET /v1/invoices' || route === 'GET /v1/promotion_codes') {
      return send(list([]))
    }

    if (route === 'GET /v1/subscriptions') {
      const customer = u.searchParams.get('customer')
      const status = u.searchParams.get('status')

      return send(
        list(
          [...subs.values()]
            .filter(
              (s) => (!customer || s.customer === customer) && (!status || status === 'all' || s.status === status),
            )
            .map((s) => shown(s, expand)),
        ),
      )
    }

    if ((m = /^(GET|POST|DELETE) \/v1\/subscriptions\/([^/]+)$/.exec(route))) {
      const sub = subs.get(m[2]!)

      if (!sub) {
        return send({ error: { code: 'resource_missing', message: 'No such subscription' } }, 404)
      }

      if (req.method === 'POST') {
        update(sub, form)
        later('customer.subscription.updated', sub)
      } else if (req.method === 'DELETE') {
        sub.status = 'canceled'
        sub.cancellation_details = { reason: 'cancellation_requested' }
        later('customer.subscription.deleted', sub)
      }

      return send(shown(sub, expand))
    }

    if ((m = /^DELETE \/v1\/subscription_items\/([^/]+)$/.exec(route))) {
      const sub = [...subs.values()].find((s) => s.items.data.some((it) => it.id === m![1]))

      if (!sub) {
        return send({ error: { message: 'Invalid subscription_item id' } }, 404)
      }

      sub.items.data = sub.items.data.filter((it) => it.id !== m![1])
      later('customer.subscription.updated', sub)

      return send({ id: m[1], deleted: true })
    }

    // One add-on on sale; its feature is granted while a live subscription holds its price.
    if (route === 'GET /v1/products') {
      return send(
        list([
          {
            id: 'prod_addon',
            name: 'AI pack',
            description: 'More AI pictures every month.',
            metadata: { staqly_kind: 'addon', staqly_addon: 'ai_pack' },
            marketing_features: [{ name: '500 AI pictures a month' }],
          },
        ]),
      )
    }

    if (route === 'GET /v1/entitlements/features') {
      return send(list([{ id: 'feat_ai_pack', lookup_key: 'ai_pack', name: 'AI pack', metadata: {} }]))
    }

    if (route === 'GET /v1/entitlements/active_entitlements') {
      return send(list(addonsOf(u.searchParams.get('customer')).map((a) => ({ feature: `feat_${a}`, lookup_key: a }))))
    }

    if (route === 'POST /v1/billing_portal/sessions') {
      return send({ url: `${url}/portal` })
    }

    unknown.push(route)
    send({ error: { type: 'invalid_request_error', message: `the Stripe stand-in has no ${route}` } }, 404)
  }

  const server = createServer((req, res) => {
    handle(req, res).catch((err: unknown) => {
      res.writeHead(500)
      res.end(String(err))
    })
  })

  await new Promise<void>((resolve, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', resolve)
  })

  return { url, close: () => new Promise<void>((r) => server.close(() => r())) }
}
