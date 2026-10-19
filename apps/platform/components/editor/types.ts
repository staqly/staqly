import type { Editor } from '@tiptap/react'

import type { RichTextEditorIcons } from './icon-types'
import type { RichTextEditorLabels } from './labels'

export type RichTextEditorVariant = 'default' | 'subtle' | 'compact'

export interface RichTextEditorProps {
  editor: Editor | null
  children: React.ReactNode
  className?: string
  labels?: Partial<RichTextEditorLabels>
  icons?: Partial<RichTextEditorIcons>
  variant?: RichTextEditorVariant
  editable?: boolean
}

export interface RichTextEditorContentProps {
  children?: React.ReactNode
  className?: string
}

export interface RichTextEditorControlsGroupProps {
  children: React.ReactNode
  className?: string
}

export interface RichTextEditorControlProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  active?: boolean
  interactive?: boolean
}
