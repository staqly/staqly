export function formatDate(iso: string | null | undefined): string {
  const d = iso ? new Date(iso) : null

  return d && !Number.isNaN(d.getTime()) ? d.toLocaleString('en', { dateStyle: 'medium', timeStyle: 'short' }) : '—'
}

export function formatMoney(minor: number, currency: string): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currency.toUpperCase(),
  }).format(minor / 100)
}

export function formatNumber(n: number): string {
  return new Intl.NumberFormat('en-US').format(n)
}

export function formatBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = n > 0 ? Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1) : 0

  return `${new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 }).format(n / 1024 ** i)} ${units[i]}`
}
