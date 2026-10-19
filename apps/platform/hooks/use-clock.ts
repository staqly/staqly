import { useEffect, useState } from 'react'

/** `t` rounded down to its local minute or hour. */
function floorTo(t: number, unit: 'minute' | 'hour') {
  const d = new Date(t)

  if (unit === 'hour') {
    d.setMinutes(0, 0, 0)
  } else {
    d.setSeconds(0, 0)
  }

  return d.getTime()
}

/**
 * The current time rounded down to the local minute or hour. One timer to the next boundary, so the component
 * renders when the shown minute (or hour) changes and every clock on the page turns over together.
 */
export function useClock(unit: 'minute' | 'hour') {
  const [now, setNow] = useState(() => floorTo(Date.now(), unit))

  useEffect(() => {
    let t: ReturnType<typeof setTimeout> | undefined

    const arm = () => {
      const current = floorTo(Date.now(), unit)
      const next = new Date(current)

      if (unit === 'hour') {
        next.setHours(next.getHours() + 1)
      } else {
        next.setMinutes(next.getMinutes() + 1)
      }

      setNow(current)
      // A few ms late, so the timer never fires just before the boundary.
      t = setTimeout(arm, next.getTime() - Date.now() + 5)
    }

    arm()

    return () => clearTimeout(t)
  }, [unit])

  return now
}
