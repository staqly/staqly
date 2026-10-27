'use client'

import { LogoMark } from '@staqly/brand/logo'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'

import { MultiSelect, type MultiOption } from '@/components/common/multi-select'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type { ProviderField } from '@/lib/api'
import { safeImageSrc } from '@/lib/safe'

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

type RemoteOption = { value: string; label: string; icon?: string }

/**
 * Where a remote field gets its choices: the composer passes the API call, so this form (also used where no API
 * call is loaded, like connecting) imports no API code.
 */
export type RemoteOptions = { channelId: string; load: (key: string) => Promise<RemoteOption[]> }

/** An option's picture: staqly's mark, or the platform's (a person's photo). */
function OptionIcon({ icon }: { icon?: string }) {
  if (!icon) {
    return null
  }

  if (icon === 'staqly') {
    return <LogoMark className="size-4" />
  }

  // A platform's picture (a person's or a channel's photo) from the API: only an http(s) or uploaded picture.
  const src = safeImageSrc(icon)

  if (!src) {
    return null
  }

  return <img src={src} alt="" width={16} height={16} className="size-4 shrink-0 rounded-full object-cover" />
}

/**
 * A remote field: values from the list the platform gives for the channel (Slack's channels, who a post goes out
 * as), else the field's own options. One value (max_items 1) is a compact select with the options' pictures;
 * several are a searchable multi-select. A #name default shows as the channel it names. Without any list it is a
 * text field.
 */
function RemoteField({
  field: f,
  id,
  value,
  error,
  remote,
  onChange,
}: {
  field: ProviderField
  id: string
  value: string
  error?: string
  remote?: RemoteOptions
  onChange: (key: string, value: string) => void
}) {
  const channelId = remote?.channelId
  const options = useQuery({
    queryKey: ['channels', channelId, 'options', f.key],
    queryFn: () => remote!.load(f.key),
    enabled: !!remote,
    staleTime: 5 * 60_000,
    retry: false,
  })
  const list: RemoteOption[] = [...(options.data ?? f.options ?? [])]
  const items = value
    .split(',')
    .map((v) => v.trim())
    .filter(Boolean)
    .map((v) => (v.startsWith('#') ? (list.find((o) => o.label === v)?.value ?? v) : v))

  // A value the list no longer has still shows, so it can be cleared.
  for (const v of items) {
    if (!list.some((o) => o.value === v)) {
      list.unshift({ value: v, label: v })
    }
  }

  // Empty, it takes the first preferred option the platform has (Slack: #random, else #general); none, the person picks.
  const preferred = !value
    ? f.prefer?.map((label) => options.data?.find((o) => o.label === label)).find(Boolean)
    : undefined

  useEffect(() => {
    if (preferred) {
      onChange(f.key, preferred.value)
    }
  }, [preferred, f.key, onChange])

  const loading = !!remote && options.isPending && !f.options?.length
  const single = f.max_items === 1
  let control

  if (single && list.length) {
    control = (
      <Select value={items[0] ?? ''} onValueChange={(v) => onChange(f.key, v)}>
        <SelectTrigger id={id} className="w-fit max-w-full min-w-48" aria-invalid={!!error} aria-label={f.label}>
          <SelectValue placeholder="Choose" />
        </SelectTrigger>
        <SelectContent>
          {list.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              <span className="flex items-center gap-2">
                <OptionIcon icon={o.icon} />
                {o.label}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    )
  } else if (list.length || loading) {
    control = (
      <MultiSelect
        id={id}
        label={f.label}
        className="w-full"
        value={items}
        onChange={(v) => onChange(f.key, (f.max_items ? v.slice(0, f.max_items) : v).join(','))}
        options={list}
        invalid={!!error}
        placeholder={loading ? 'Loading...' : 'Choose'}
        disabled={loading}
      />
    )
  } else {
    control = (
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(f.key, e.target.value)}
        aria-invalid={!!error}
        placeholder={f.placeholder ?? undefined}
      />
    )
  }

  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={id} required={f.required}>
        {f.label}
        {!f.required && !f.default && <span className="-ml-1 font-normal text-muted-foreground">(optional)</span>}
      </FieldLabel>
      {control}
      {f.help && <FieldDescription className="text-xs">{f.help}</FieldDescription>}
      {options.isError && (
        <FieldDescription className="text-xs">The list did not load: {options.error.message}</FieldDescription>
      )}
      <FieldError>{error}</FieldError>
    </Field>
  )
}

export function ProviderFields({
  fields,
  values,
  onChange,
  errors,
  idPrefix,
  channelOptions = NO_CHANNELS,
  remote,
}: {
  fields: ProviderField[]
  values: Record<string, string>
  onChange: (key: string, value: string) => void
  errors?: Record<string, string | undefined>
  idPrefix: string
  /** The choices of a channels field: other connected channels of the same platform. */
  channelOptions?: MultiOption[]
  /** The channel the settings are for: a remote field asks its platform for the choices. */
  remote?: RemoteOptions
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

        if (f.kind === 'remote') {
          return (
            <RemoteField
              key={f.key}
              field={f}
              id={id}
              value={value}
              error={error}
              remote={remote}
              onChange={onChange}
            />
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
