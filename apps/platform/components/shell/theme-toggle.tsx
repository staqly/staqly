import { Monitor, Moon, Sun } from 'lucide-react'

/** Theme choices for Settings → Appearance and the command palette. */
export const THEMES = [
  { value: 'light', label: 'Light', Icon: Sun },
  { value: 'dark', label: 'Dark', Icon: Moon },
  { value: 'system', label: 'System', Icon: Monitor },
] as const
