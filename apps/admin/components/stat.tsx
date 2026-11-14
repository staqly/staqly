import { Card } from '@/components/ui/card'
import { formatNumber } from '@/lib/format'

export function Stat({ label, value, hint }: { label: string; value: number | string; hint?: string }) {
  return (
    <Card className="gap-1 px-4 py-4">
      <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{label}</p>
      <p className="tabular text-2xl font-semibold tracking-tight">
        {typeof value === 'number' ? formatNumber(value) : value}
      </p>
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </Card>
  )
}
