'use client'

import { Monitor, Moon, Sun } from 'lucide-react'
import { useTheme } from 'next-themes'

/** Theme choices for Settings → Appearance and the person's menu. */
export const THEMES = [
  { value: 'light', label: 'Light', Icon: Sun },
  { value: 'dark', label: 'Dark', Icon: Moon },
  { value: 'system', label: 'System', Icon: Monitor },
] as const

/** Light / Dark / System as one row of the person's menu: picking one keeps the menu open. */
export function ThemeSwitcher() {
  const { theme = 'system', setTheme } = useTheme()

  return (
    <div className="flex items-center justify-between gap-3 px-1.5 py-1">
      <span aria-hidden className="text-sm">
        Theme
      </span>
      <fieldset className="flex items-center gap-0.5 rounded-lg bg-muted p-0.5">
        <legend className="sr-only">Theme</legend>
        {THEMES.map(({ value, label, Icon }) => (
          <button
            key={value}
            type="button"
            // A menu may hold only menu items, so these are its radio items.
            role="menuitemradio"
            aria-checked={theme === value}
            aria-label={label}
            title={label}
            onClick={() => setTheme(value)}
            className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring aria-checked:bg-background aria-checked:text-foreground pointer-coarse:size-10 [&_svg]:size-4"
          >
            <Icon />
          </button>
        ))}
      </fieldset>
    </div>
  )
}
