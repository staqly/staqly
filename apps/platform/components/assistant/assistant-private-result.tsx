import { SecretField } from '@/components/common/secret-field'

function privateResult(name: string, result?: string) {
  const schedule = name === 'create_schedule' || name === 'update_schedule'

  if (!result || (!schedule && name !== 'create_channel_link' && name !== 'create_preview_link')) {
    return null
  }

  let out: Record<string, unknown> | null

  try {
    out = JSON.parse(result)
  } catch {
    return null
  }

  const value = out?.[schedule ? 'endpoint_secret' : 'url']

  if (typeof value !== 'string' || !value || (schedule && value.startsWith('****'))) {
    return null
  }

  return { value, label: schedule ? 'Endpoint secret' : 'Private link' }
}

export function AssistantPrivateResult({ name, result }: { name: string; result?: string }) {
  const secret = privateResult(name, result)

  if (!secret) {
    return null
  }

  return (
    <div className="mt-2 grid min-w-0 gap-1.5 rounded-lg border bg-muted/40 p-3 text-xs">
      <p className="font-medium">{secret.label}</p>
      <SecretField value={secret.value} label={secret.label} masked />
      <p className="text-muted-foreground">Copy this private value here. It is withheld from the AI model.</p>
    </div>
  )
}
