import { UNLIMITED } from './api'
import { TONES, type Tone } from './tones'

export type MeterLevel = 'ok' | 'warn' | 'full' | 'unlimited'

/**
 * One reading of a plan limit, for every usage meter (Plan & usage, header chips, the sidebar). A limit of
 * UNLIMITED (-1, any negative) or a missing one (older servers) is unlimited; 0 means none allowed, so the
 * meter is full. Above 80% it warns.
 */
export function usageMeter(used: number, max: number | null | undefined): { level: MeterLevel; pct: number } {
  if (max === null || max === undefined || max === UNLIMITED || max < 0) {
    return { level: 'unlimited', pct: 0 }
  }

  if (max === 0 || used >= max) {
    return { level: 'full', pct: 100 }
  }

  const pct = Math.min(100, (used / max) * 100)

  return { level: pct > 80 ? 'warn' : 'ok', pct }
}

const LEVEL_TONE: Record<Exclude<MeterLevel, 'unlimited'>, Tone> = { ok: 'success', warn: 'warning', full: 'danger' }

/** The tone classes of a meter level (unlimited meters have no fill). */
export function meterTone(level: MeterLevel) {
  return TONES[level === 'unlimited' ? 'neutral' : LEVEL_TONE[level]]
}
