import { useEffect, useMemo } from 'react'

import { DEFAULT_ICONS } from './icons'
import { DEFAULT_LABELS } from './labels'
import { RichTextEditorContext } from './rte-context'
import type { RichTextEditorProps } from './types'
import { cn } from './ui/utils'

// staqly: the root on its own, so an editor that imports its parts directly (components/posts/post-editor.tsx)
// leaves out what the RichTextEditor.* statics hold (the bubble menu, every control, the footer).
export const RichTextEditorRoot = ({
  editor,
  children,
  className,
  labels,
  icons,
  variant = 'default',
  editable = true,
}: RichTextEditorProps) => {
  const mergedLabels = useMemo(() => ({ ...DEFAULT_LABELS, ...labels }), [labels])

  const mergedIcons = useMemo(() => ({ ...DEFAULT_ICONS, ...icons }), [icons])

  useEffect(() => {
    if (editor && editor.isEditable !== editable) {
      editor.setEditable(editable)
    }
  }, [editor, editable])

  return (
    <RichTextEditorContext.Provider
      value={{
        editable,
        editor,
        icons: mergedIcons,
        labels: mergedLabels,
        variant,
      }}
    >
      <div
        className={cn('rte-root', variant !== 'default' && `rte-root--${variant}`, className)}
        data-variant={variant}
      >
        {children}
      </div>
    </RichTextEditorContext.Provider>
  )
}
