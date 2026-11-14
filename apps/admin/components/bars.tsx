import { formatNumber } from '@/lib/format'

export function Bars({ data, label }: { data: { day: string; count: number }[]; label: string }) {
  const max = Math.max(1, ...data.map((d) => d.count))
  const total = data.reduce((n, d) => n + d.count, 0)

  return (
    <figure className="m-0">
      <div
        role="img"
        aria-label={`${label}: ${formatNumber(total)} in ${data.length} days`}
        className="flex h-32 items-end gap-0.5"
      >
        {data.map((d) => (
          <div
            key={d.day}
            title={`${d.day}: ${formatNumber(d.count)}`}
            className="flex h-full min-w-0 flex-1 items-end"
          >
            <div
              className="w-full rounded-t-sm bg-primary/80 transition-colors hover:bg-primary"
              style={{
                height: `${d.count === 0 ? 2 : Math.max(4, (d.count / max) * 100)}%`,
                opacity: d.count === 0 ? 0.25 : 1,
              }}
            />
          </div>
        ))}
      </div>
      <figcaption className="mt-2 flex justify-between text-xs text-muted-foreground">
        <span>{data[0]?.day}</span>
        <span className="tabular">peak {formatNumber(max === 1 && total === 0 ? 0 : max)}</span>
        <span>{data.at(-1)?.day}</span>
      </figcaption>
    </figure>
  )
}
