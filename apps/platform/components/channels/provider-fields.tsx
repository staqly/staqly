'use client'

import { MultiSelect, type MultiOption } from '@/components/common/multi-select'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { ProviderField } from '@/lib/api'

/**
 * The form a provider describes (its credential fields when connecting, its per-post settings in the
 * composer), rendered from the catalog so a new provider needs no dashboard code. Values are strings
 * (a boolean is "true" / "false"), as the API stores them.
 */
/** The fields in play: a field with `when` shows only while the other field (or its default) has that value. */
export function visibleFields(fields: ProviderField[], values: Record<string, string>) {
  const valueOf = (key: string) => values[key] ?? fields.find((f) => f.key === key)?.default ?? ''

  return fields.filter((f) => !f.when || valueOf(f.when.key) === f.when.value)
}

/** The values to send: what was typed plus the defaults of visible fields left alone (a select shows its default). */
export function fieldValues(fields: ProviderField[], values: Record<string, string>) {
  const out: Record<string, string> = {}

  for (const f of visibleFields(fields, values)) {
    const v = values[f.key] ?? f.default

    if (v) {
      out[f.key] = v
    }
  }

  return out
}

const NO_CHANNELS: MultiOption[] = []

export function ProviderFields({
  fields,
  values,
  onChange,
  errors,
  idPrefix,
  channelOptions = NO_CHANNELS,
}: {
  fields: ProviderField[]
  values: Record<string, string>
  onChange: (key: string, value: string) => void
  errors?: Record<string, string | undefined>
  idPrefix: string
  /** The choices of a channels field: other connected channels of the same platform. */
  channelOptions?: MultiOption[]
}) {
  const shown = visibleFields(fields, values)

  if (!shown.length) {
    return null
  }

  // An HTTP endpoint (your own automation): the method sits left of the URL on one row, like an API client.
  const method = shown.find((f) => f.key === 'method' && f.kind === 'select')
  const hasUrl = shown.some((f) => f.key === 'url')

  return (
    <div className="grid gap-4">
      {shown.map((f) => {
        const id = `${idPrefix}-${f.key}`
        const value = values[f.key] ?? f.default ?? ''
        const error = errors?.[f.key]

        if (method && hasUrl && f === method) {
          return null
        }

        if (method && f.key === 'url') {
          const methodId = `${idPrefix}-method`
          const methodError = errors?.method

          return (
            <Field key={f.key} data-invalid={!!error || !!methodError}>
              <FieldLabel htmlFor={id} required={f.required}>
                {f.label}
                {!f.required && !(f.kind === 'select' && f.default) && (
                  <span className="-ml-1 font-normal text-muted-foreground">(optional)</span>
                )}
              </FieldLabel>
              <div className="flex gap-2">
                <Select value={values.method ?? method.default ?? ''} onValueChange={(v) => onChange('method', v)}>
                  <SelectTrigger
                    id={methodId}
                    className="w-28 shrink-0 font-mono text-xs"
                    aria-invalid={!!methodError}
                    aria-label={method.label}
                  >
                    <SelectValue placeholder="Method" />
                  </SelectTrigger>
                  <SelectContent>
                    {(method.options ?? []).map((o) => (
                      <SelectItem key={o.value} value={o.value} className="font-mono text-xs">
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  id={id}
                  type="url"
                  value={value}
                  onChange={(e) => onChange(f.key, e.target.value)}
                  aria-invalid={!!error}
                  aria-required={f.required}
                  placeholder={f.placeholder ?? undefined}
                  className="min-w-0 flex-1"
                />
              </div>
              {(f.help || method.help) && (
                <FieldDescription className="text-xs">
                  {[f.help, method.help].filter(Boolean).join(' ')}
                </FieldDescription>
              )}
              <FieldError>{error ?? methodError}</FieldError>
            </Field>
          )
        }

        // Comma-separated values: free ones (tags, usernames) or other channels, picked from a list.
        if (f.kind === 'list' || f.kind === 'channels') {
          const items = value
            .split(',')
            .map((v) => v.trim())
            .filter(Boolean)
          const options =
            f.kind === 'channels' ? channelOptions : items.map((v) => ({ value: v, label: v }) satisfies MultiOption)

          return (
            <Field key={f.key} data-invalid={!!error}>
              <FieldLabel htmlFor={id} required={f.required}>
                {f.label}
                {!f.required && <span className="-ml-1 font-normal text-muted-foreground">(optional)</span>}
              </FieldLabel>
              <MultiSelect
                id={id}
                label={f.label}
                className="w-full"
                value={items}
                onChange={(v) => onChange(f.key, (f.max_items ? v.slice(0, f.max_items) : v).join(','))}
                options={options}
                invalid={!!error}
                placeholder={
                  f.kind === 'channels'
                    ? channelOptions.length
                      ? 'Choose accounts'
                      : 'No other accounts on this platform'
                    : (f.placeholder ?? 'Add')
                }
                disabled={f.kind === 'channels' && !channelOptions.length}
                creatable={f.kind === 'list' ? { normalize: (v) => v.trim().replace(/,/g, '') } : undefined}
              />
              {f.help && <FieldDescription className="text-xs">{f.help}</FieldDescription>}
              <FieldError>{error}</FieldError>
            </Field>
          )
        }

        if (f.kind === 'boolean') {
          return (
            <Field key={f.key} data-invalid={!!error}>
              <div className="flex items-center gap-3">
                <Switch
                  id={id}
                  checked={value === 'true'}
                  onCheckedChange={(on) => onChange(f.key, on ? 'true' : 'false')}
                />
                <FieldLabel htmlFor={id}>{f.label}</FieldLabel>
              </div>
              {f.help && <FieldDescription className="text-xs">{f.help}</FieldDescription>}
              <FieldError>{error}</FieldError>
            </Field>
          )
        }

        return (
          <Field key={f.key} data-invalid={!!error}>
            <FieldLabel htmlFor={id} required={f.required}>
              {f.label}
              {!f.required && !(f.kind === 'select' && f.default) && (
                <span className="-ml-1 font-normal text-muted-foreground">(optional)</span>
              )}
            </FieldLabel>
            {f.kind === 'select' ? (
              <Select value={value} onValueChange={(v) => onChange(f.key, v)}>
                <SelectTrigger id={id} className="w-full" aria-invalid={!!error} aria-label={f.label}>
                  <SelectValue placeholder="Choose" />
                </SelectTrigger>
                <SelectContent>
                  {(f.options ?? []).map((o) => (
                    <SelectItem key={o.value} value={o.value}>
                      {o.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : f.kind === 'textarea' ? (
              <Textarea
                id={id}
                rows={3}
                value={value}
                onChange={(e) => onChange(f.key, e.target.value)}
                aria-invalid={!!error}
                aria-required={f.required}
                placeholder={f.placeholder ?? undefined}
              />
            ) : (
              <Input
                id={id}
                type={f.kind === 'secret' ? 'password' : f.kind === 'url' ? 'url' : 'text'}
                autoComplete={f.kind === 'secret' ? 'off' : undefined}
                value={value}
                onChange={(e) => onChange(f.key, e.target.value)}
                aria-invalid={!!error}
                aria-required={f.required}
                placeholder={f.placeholder ?? undefined}
              />
            )}
            {f.help && <FieldDescription className="text-xs">{f.help}</FieldDescription>}
            <FieldError>{error}</FieldError>
          </Field>
        )
      })}
    </div>
  )
}
