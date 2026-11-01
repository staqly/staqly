'use client'

import { useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { emptyPromo, promoPayload, serverPromoErrors, validatePromo, type PromoForm } from '@/lib/promo'

function Field({ id, label, error, children }: { id: string; label: string; error?: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}

const SELECT =
  'h-9 w-full rounded-lg border border-input bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring'

export function PromoFormView({
  busy,
  serverError,
  serverFields,
  onSubmit,
}: {
  busy: boolean
  serverError?: string
  serverFields?: Record<string, string>
  onSubmit: (body: Record<string, unknown>) => void
}) {
  const [f, setF] = useState<PromoForm>(emptyPromo)
  const [local, setErrors] = useState<Record<string, string>>({})
  const mapped = serverPromoErrors(serverFields ?? {}, f.kind)
  const errors = { ...mapped, ...local }
  const set = (patch: Partial<PromoForm>) => setF((p) => ({ ...p, ...patch }))
  const err = (k: string) => ({
    'aria-invalid': errors[k] ? true : undefined,
    'aria-describedby': errors[k] ? `pf-${k}-error` : undefined,
  })

  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={(e) => {
        e.preventDefault()

        const v = validatePromo(f)

        setErrors(v)

        if (Object.keys(v).length === 0) {
          onSubmit(promoPayload(f))
        }
      }}
    >
      <Field id="pf-code" label="Code" error={errors.code}>
        <Input
          id="pf-code"
          value={f.code}
          onChange={(e) => set({ code: e.target.value.toUpperCase() })}
          className="font-mono"
          autoComplete="off"
          {...err('code')}
        />
      </Field>
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">Discount</legend>
        <div className="flex gap-4 text-sm">
          {(['percent', 'amount'] as const).map((k) => (
            <label key={k} className="flex items-center gap-2">
              <input type="radio" name="kind" checked={f.kind === k} onChange={() => set({ kind: k })} />
              {k === 'percent' ? 'Percent off' : 'Amount off'}
            </label>
          ))}
        </div>
      </fieldset>
      {f.kind === 'percent' ? (
        <Field id="pf-percent" label="Percent off (1–100)" error={errors.percent}>
          <Input
            id="pf-percent"
            inputMode="decimal"
            value={f.percent}
            onChange={(e) => set({ percent: e.target.value })}
            {...err('percent')}
          />
        </Field>
      ) : (
        <div className="grid grid-cols-[1fr_6rem] gap-3">
          <Field id="pf-amount" label="Amount off" error={errors.amount}>
            <Input
              id="pf-amount"
              inputMode="decimal"
              value={f.amount}
              onChange={(e) => set({ amount: e.target.value })}
              {...err('amount')}
            />
          </Field>
          <Field id="pf-currency" label="Currency" error={errors.currency}>
            <Input
              id="pf-currency"
              value={f.currency}
              maxLength={3}
              onChange={(e) => set({ currency: e.target.value })}
              {...err('currency')}
            />
          </Field>
        </div>
      )}
      <Field id="pf-duration" label="Duration">
        <select
          id="pf-duration"
          className={SELECT}
          value={f.duration}
          onChange={(e) => set({ duration: e.target.value as PromoForm['duration'] })}
        >
          <option value="once">Once</option>
          <option value="repeating">Repeating</option>
          <option value="forever">Forever</option>
        </select>
      </Field>
      {f.duration === 'repeating' && (
        <Field id="pf-months" label="Months (1–36)" error={errors.months}>
          <Input
            id="pf-months"
            inputMode="numeric"
            value={f.months}
            onChange={(e) => set({ months: e.target.value })}
            {...err('months')}
          />
        </Field>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field id="pf-max" label="Max redemptions (optional)" error={errors.maxRedemptions}>
          <Input
            id="pf-max"
            inputMode="numeric"
            value={f.maxRedemptions}
            onChange={(e) => set({ maxRedemptions: e.target.value })}
            {...err('maxRedemptions')}
          />
        </Field>
        <Field id="pf-expires" label="Expires (optional)" error={errors.expiresAt}>
          <Input
            id="pf-expires"
            type="date"
            value={f.expiresAt}
            onChange={(e) => set({ expiresAt: e.target.value })}
            {...err('expiresAt')}
          />
        </Field>
      </div>
      {serverError && Object.keys(mapped).length === 0 && (
        <p role="alert" className="text-sm text-destructive">
          {serverError}
        </p>
      )}
      <Button type="submit" disabled={busy} className="w-full">
        {busy ? 'Creating…' : 'Create code'}
      </Button>
    </form>
  )
}
