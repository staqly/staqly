'use client'

import { useState } from 'react'
import type { FieldValues, Path, UseFormSetError } from 'react-hook-form'

import { ApiError } from './api'
import { toastApiError } from './notify'

/**
 * Every form in the dashboard shows errors the same way:
 *
 * - required fields: `required` on the Label/FieldLabel (red `*`) and `aria-required` on the control;
 * - an invalid field: `data-invalid` on its Field (red label), `aria-invalid` on the control (red border)
 *   and the message right under it (FieldError);
 * - errors appear on submit and then follow every change, so they clear once the field is fixed;
 * - a 422 from the API lands under the field it names; only errors without a field are toasted.
 *
 * React Hook Form gives the timing for free (mode `onSubmit`, re-validate on change). Hand-built forms use
 * `useFormErrors` below for the same behaviour.
 */

export type FieldErrorMap<K extends string = string> = Partial<Record<K, string>>

/** API messages are written to follow the field name ("is required"); show them as a sentence. */
export function fieldMessage(msg: string): string {
  const m = msg.trim()

  if (!m) {
    return 'Invalid value'
  }

  const sentence = /^(is|are)\b/i.test(m) ? `This field ${m}` : m

  return sentence.charAt(0).toUpperCase() + sentence.slice(1)
}

/**
 * The field errors of a failed request, keyed by form field. `map` turns an API field name
 * (e.g. `config.chat_id`) into a form field, or returns null when the form has no such field.
 * `complete` is false when an API field error found no form field (then it still needs a toast).
 */
export function apiFieldErrors<K extends string>(
  err: unknown,
  map: (apiField: string) => K | null | undefined,
): { errors: FieldErrorMap<K>; complete: boolean } {
  const errors: FieldErrorMap<K> = {}

  if (!(err instanceof ApiError) || !err.fields) {
    return { errors, complete: false }
  }

  let complete = Object.keys(err.fields).length > 0

  for (const [k, msg] of Object.entries(err.fields)) {
    const target = map(k)

    if (target) {
      errors[target] ??= fieldMessage(msg)
    } else {
      complete = false
    }
  }

  return { errors, complete }
}

/**
 * Handles a failed submit: returns the field errors to show under the fields, and toasts the error
 * unless every part of it belongs to a field.
 */
export function formApiError<K extends string>(
  err: unknown,
  map: (apiField: string) => K | null | undefined,
  fallbackTitle?: string,
): FieldErrorMap<K> {
  const { errors, complete } = apiFieldErrors(err, map)

  if (!complete) {
    toastApiError(err, fallbackTitle)
  }

  return errors
}

/** A field of the form, or a `root.*` error shown for a group of fields. */
export type FormErrorPath<T extends FieldValues> = Path<T> | `root.${string}`

/** `formApiError` for React Hook Form: puts the messages on the fields with `setError` (first one focused). */
export function setFormApiError<T extends FieldValues>(
  err: unknown,
  setError: UseFormSetError<T>,
  map: (apiField: string) => FormErrorPath<T> | null | undefined,
  fallbackTitle?: string,
) {
  const errors = formApiError(err, map, fallbackTitle)
  let first = true

  for (const [k, message] of Object.entries(errors) as [FormErrorPath<T>, string][]) {
    setError(k, { type: 'server', message }, { shouldFocus: first })
    first = false
  }
}

/** A `map` for `formApiError` that accepts exactly these field names. */
export function fieldsOf<K extends string>(...names: K[]) {
  return (apiField: string): K | null => (names.includes(apiField as K) ? (apiField as K) : null)
}

/** After a failed submit: move focus to the first invalid control, like React Hook Form does. */
export function focusFirstInvalid(root: HTMLElement | null | undefined) {
  if (!root) {
    return
  }

  requestAnimationFrame(() => {
    root.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  })
}

/**
 * Error state for a hand-built form, with React Hook Form's timing. Pass the current client errors
 * (`validate(values)`, computed on every render); they are shown only after the first submit and then
 * follow every change. Server (422) errors stay until their field changes (`touch(field)`).
 */
export function useFormErrors<K extends string>(clientErrors: FieldErrorMap<K>) {
  const [submitted, setSubmitted] = useState(false)
  const [server, setServer] = useState<FieldErrorMap<K>>({})
  // Only the client errors that are set: `{ name: undefined }` (a field that passes) must not hide its server error.
  const failing = Object.entries(clientErrors).filter(([, v]) => v) as [K, string][]
  const errors: FieldErrorMap<K> = submitted ? { ...server, ...Object.fromEntries(failing) } : server
  const valid = failing.length === 0

  return {
    errors,
    /** Call at the start of submit; returns true when the client-side checks pass. */
    submit() {
      setSubmitted(true)
      setServer({})

      return valid
    },
    /** A field changed: its server error no longer applies. */
    touch(field: K) {
      setServer((prev) => {
        if (!(field in prev)) {
          return prev
        }

        const next = { ...prev }

        delete next[field]

        return next
      })
    },
    /** Show the field errors of a failed request (from `formApiError`). */
    setServerErrors: setServer,
    /** Back to a pristine form (e.g. when the form switches mode). */
    reset() {
      setSubmitted(false)
      setServer({})
    },
  }
}
