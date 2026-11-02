'use client'

import { useEffect, useState } from 'react'

import { env } from './env'
import { COMPARE, PLANS, type Cell, type SitePlan } from './plans'

/** A plan as GET /v1/plans answers (the fields the site reads). */
export type ApiPlan = {
  id: string
  price_month: number
  price_year: number
  features: string[]
  limits: Record<string, number | boolean>
}

const GB = 1024 ** 3
const MB = 1024 ** 2

/** A limit as the comparison table shows it ("1,500", "10 GB", "Unlimited", a check). */
export function limitCell(key: string, v: number | boolean): Cell {
  if (typeof v === 'boolean') {
    return v
  }

  if (v === -1) {
    return 'Unlimited'
  }

  if (key === 'storage_bytes' || key === 'max_upload_bytes') {
    return v >= GB && v % GB === 0 ? `${v / GB} GB` : `${Math.round(v / MB)} MB`
  }

  return v.toLocaleString('en-US')
}

/** The static plans and comparison with the live prices, features and limits put in (by plan id). */
export function withLive(api: ApiPlan[]) {
  const byID = new Map(api.map((p) => [p.id, p]))
  const plans: SitePlan[] = PLANS.map((p) => {
    const live = byID.get(p.id)

    return live
      ? {
          ...p,
          month: live.price_month,
          year: live.price_year,
          features: live.features.length ? live.features : p.features,
        }
      : p
  })
  const compare = COMPARE.map((section) => ({
    ...section,
    rows: section.rows.map((row) => {
      const key = row.limit

      if (!key) {
        return row
      }

      const values = PLANS.map((p, i) => {
        const v = byID.get(p.id)?.limits[key]

        return v === undefined ? row.values[i]! : limitCell(key, v)
      }) as typeof row.values

      return { ...row, values }
    }),
  }))

  return { plans, compare }
}

const STATIC = { plans: PLANS, compare: COMPARE }

/**
 * The plans for the pricing page: the static ones at once, then the live ones from the dashboard's public API once
 * they arrive. A failed request (offline, the API down) keeps the static ones.
 */
export function useLivePlans() {
  const [data, setData] = useState(STATIC)

  useEffect(() => {
    const ctl = new AbortController()

    fetch(`${env.appUrl}/api/v1/plans`, { signal: ctl.signal, credentials: 'omit' })
      .then((r) => (r.ok ? (r.json() as Promise<{ data: ApiPlan[] }>) : Promise.reject(new Error(String(r.status)))))
      .then((r) => setData(withLive(r.data)))
      .catch(() => {})

    return () => ctl.abort()
  }, [])

  return data
}
