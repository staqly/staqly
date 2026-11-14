import { useEffect, useState } from 'react'

import { wallOf } from '@/lib/zone'

/** `t` rounded down to its minute or to its hour in the workspace's zone (half-hour zones turn at :30). */
function floorTo(t: number, unit: 'minute' | 'hour') {
  const minute = Math.floor(t / 60_000) * 60_000

  return unit === 'hour' ? minute - wallOf(t).minute * 60_000 : minute
}

/**
 * The current time rounded down to the minute or the workspace's hour. One timer to the next boundary, so the component
 * renders when the shown minute (or hour) changes and every clock on the page turns over together.
 */
export function useClock(unit: 'minute' | 'hour') {
  const [now, setNow] = useState(() => floorTo(Date.now(), unit))

  useEffect(() => {
    let t: ReturnType<typeof setTimeout> | undefined

    const arm = () => {
      const current = floorTo(Date.now(), unit)
      const next = current + (unit === 'hour' ? 3_600_000 : 60_000)

      setNow(current)
      // A few ms late, so the timer never fires just before the boundary.
      t = setTimeout(arm, next - Date.now() + 5)
    }

    arm()

    return () => clearTimeout(t)
  }, [unit])

  return now
}
