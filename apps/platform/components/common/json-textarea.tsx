'use client'

import { useRef } from 'react'

import { jsonTokens } from '@/components/common/json-code'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/** The text pretty-printed with two spaces, or null when it is not JSON. */
function pretty(text: string): string | null {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return null
  }
}

type Props = Omit<React.ComponentProps<'textarea'>, 'value' | 'onChange' | 'defaultValue' | 'rows'> & {
  value: string
  onChange: (value: string) => void
}

// The three layers (gutter, coloured copy, textarea) share the font and the padding so the characters line up.
const FONT = 'font-mono text-xs leading-5'
const PAD = 'py-2 pr-2.5 pl-11'

/**
 * A JSON field: monospace, no wrapping, a line-number gutter, syntax colours, 4 to 10 lines tall (it grows with
 * the text, then scrolls). JSON pasted over the whole field is pretty-printed at once; a Format button does the
 * same for text typed by hand. Nothing is reformatted on blur: the field would grow under a button the user is
 * about to click. Broken JSON stays as typed so the error points at what was written. The colours come from a
 * read-only copy under the textarea, whose own text is transparent.
 */
export function JsonTextarea({ className, value, onChange, readOnly, 'aria-invalid': invalid, ...props }: Props) {
  const gutter = useRef<HTMLDivElement>(null)
  const code = useRef<HTMLPreElement>(null)
  const lines = value.split('\n').length
  const formatted = readOnly ? null : pretty(value)
  const canFormat = formatted !== null && formatted !== value

  return (
    <div
      data-slot="json-textarea"
      data-error={invalid ? '' : undefined}
      className={cn(
        'relative overflow-hidden rounded-lg border border-input bg-transparent transition-colors focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background data-error:border-destructive data-error:ring-3 data-error:ring-destructive/20 dark:bg-input/30 dark:data-error:border-destructive/50 dark:data-error:ring-destructive/40',
        FONT,
        className,
      )}
    >
      <div
        ref={gutter}
        aria-hidden
        className="pointer-events-none absolute inset-y-0 left-0 w-9 overflow-hidden border-r border-input bg-muted/40 py-2 pr-1.5 text-right text-muted-foreground/70 select-none"
      >
        {Array.from({ length: lines }, (_, i) => (
          <div key={i}>{i + 1}</div>
        ))}
      </div>
      <pre
        ref={code}
        aria-hidden
        className={cn('pointer-events-none absolute inset-0 overflow-hidden whitespace-pre text-foreground', FONT, PAD)}
      >
        <code>
          {jsonTokens(value)}
          {'\n'}
        </code>
      </pre>
      <textarea
        value={value}
        readOnly={readOnly}
        aria-invalid={invalid}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        wrap="off"
        onChange={(e) => onChange(e.target.value)}
        onPaste={(e) => {
          const el = e.currentTarget
          const whole = el.selectionStart === 0 && el.selectionEnd === value.length
          const next = whole ? pretty(e.clipboardData.getData('text')) : null

          if (next !== null && !readOnly) {
            e.preventDefault()
            onChange(next)
          }
        }}
        onScroll={(e) => {
          const { scrollTop, scrollLeft } = e.currentTarget

          if (gutter.current) {
            gutter.current.scrollTop = scrollTop
          }

          if (code.current) {
            code.current.scrollTop = scrollTop
            code.current.scrollLeft = scrollLeft
          }
        }}
        className={cn(
          'relative block field-sizing-content max-h-[calc(10*1.25rem+1rem)] min-h-[calc(4*1.25rem+1rem)] w-full resize-none overflow-auto bg-transparent whitespace-pre text-transparent caret-foreground outline-none placeholder:text-muted-foreground read-only:caret-transparent disabled:cursor-not-allowed disabled:opacity-50',
          FONT,
          PAD,
        )}
        {...props}
      />
      {canFormat && (
        <Button
          type="button"
          size="xs"
          variant="ghost"
          aria-label="Format JSON"
          className="absolute top-1 right-1 font-sans text-muted-foreground"
          onClick={() => onChange(formatted)}
        >
          Format
        </Button>
      )}
    </div>
  )
}
