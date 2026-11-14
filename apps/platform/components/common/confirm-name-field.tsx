'use client'

import { useState } from 'react'

import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'

import { ConfirmValue } from './confirm-value'

/** The message shown when the typed name does not match (on submit). */
export function confirmNameError(typed: string, expected: string): string | undefined {
  return typed === expected && expected !== '' ? undefined : `Type “${expected}” exactly to confirm`
}

/**
 * "Type the name to confirm" field of a destructive dialog. The submit button stays enabled; a
 * mismatch is reported under the field on submit (see `confirmNameError`).
 */
export function ConfirmNameField({
  id,
  label,
  expected,
  value,
  onChange,
  error,
}: {
  id: string
  label: string
  expected: string
  value: string
  onChange: (v: string) => void
  error?: string
}) {
  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={id} required>
        {label}
      </FieldLabel>
      <ConfirmValue value={expected} />
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={!!error}
        aria-required
        autoComplete="off"
        spellCheck={false}
        autoCapitalize="off"
      />
      <FieldError>{error}</FieldError>
    </Field>
  )
}

/**
 * A required "I have saved/understood..." checkbox. The button it guards stays enabled; pressing it
 * unchecked shows `error` under the checkbox.
 */
export function AckCheckbox({
  id,
  checked,
  onCheckedChange,
  children,
  error,
  className,
}: {
  id: string
  checked: boolean
  onCheckedChange: (v: boolean) => void
  children: React.ReactNode
  error?: string
  className?: string
}) {
  return (
    <Field data-invalid={!!error} className={className}>
      <div className="flex items-start gap-3">
        <Checkbox
          id={id}
          checked={checked}
          onCheckedChange={(v) => onCheckedChange(v === true)}
          aria-invalid={!!error}
          aria-required
          className="mt-0.5"
        />
        <FieldLabel htmlFor={id} required className="flex-1 leading-snug font-normal">
          {children}
        </FieldLabel>
      </div>
      <FieldError>{error}</FieldError>
    </Field>
  )
}

/**
 * State of an `AckCheckbox` that guards a button ("I have saved my recovery key" before leaving the page or
 * closing the dialog). Spread `checkbox` on the AckCheckbox; the button calls `confirm(next)`, which runs `next`
 * when checked and otherwise shows `message` under the checkbox and focuses it.
 */
export function useAckGate(id: string, message: string) {
  const [checked, setChecked] = useState(false)
  // The guarded button was pressed: from then on an unchecked box shows why nothing happened.
  const [tried, setTried] = useState(false)

  return {
    checked,
    checkbox: { id, checked, onCheckedChange: setChecked, error: tried && !checked ? message : undefined },
    confirm(next: () => void) {
      setTried(true)

      if (checked) {
        next()
      } else {
        document.getElementById(id)?.focus()
      }
    },
    reset() {
      setChecked(false)
      setTried(false)
    },
  }
}
