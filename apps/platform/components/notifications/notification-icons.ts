import { AlertTriangle, CheckCircle2, Info, XCircle } from 'lucide-react'

import type { Tone } from '@/lib/tones'

/** A notification's icon by its tone (the bell's rows and the Notifications page). */
export const ICONS: Record<Tone, typeof Info> = {
  success: CheckCircle2,
  danger: XCircle,
  warning: AlertTriangle,
  info: Info,
  neutral: Info,
  attention: AlertTriangle,
  accent: Info,
}
