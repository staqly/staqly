import { useEffect, useState } from 'react'

/** The current time in ms, ticking every `ms` while `active` (countdowns, elapsed timers). */
export function useNow(active = true, ms = 1000) {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!active) {
      return
    }

    const t = setInterval(() => setNow(Date.now()), ms)

    return () => clearInterval(t)
  }, [active, ms])

  return now
}
