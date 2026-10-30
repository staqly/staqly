'use client'

import { Placeholder } from '@tiptap/extension-placeholder'
import { useEditor } from '@tiptap/react'
import { StarterKit } from '@tiptap/starter-kit'
import { useEffect, useRef, useState } from 'react'

// The parts this editor uses, not the `components/editor` index: its RichTextEditor.* statics would bundle the
// bubble menu and every other control into the editor's chunk.
import {
  BoldControl,
  BulletListControl,
  ItalicControl,
  OrderedListControl,
  RedoControl,
  UndoControl,
  UnlinkControl,
} from '@/components/editor/controls/rte-controls'
import { LinkControl } from '@/components/editor/controls/rte-link-control'
import { Link } from '@/components/editor/extensions'
import { Content } from '@/components/editor/rte-content'
import { ControlsGroup } from '@/components/editor/rte-controls-group'
import { RichTextEditorRoot } from '@/components/editor/rte-root'

import '@/components/editor/style.css'
import { docToText, textToHtml } from '@/lib/post-text'
import { cn } from '@/lib/utils'

/**
 * The post text field: editorcn's toolbar editor (Tiptap) over plain text. Bold and italic become Unicode
 * letters, lists become "•" or "1." lines (lib/post-text). `value` stays the source of truth: a change from
 * outside (another post, a reset) replaces the document, once the editor shows (`hidden` ones wait).
 */
export function PostEditor({
  id,
  value,
  onChange,
  placeholder,
  labelledBy,
  invalid,
  required,
  testId,
  className,
  editable = true,
  hidden = false,
  below,
  start,
  end,
}: {
  id: string
  value: string
  onChange: (text: string) => void
  placeholder?: string
  labelledBy?: string
  invalid?: boolean
  required?: boolean
  testId?: string
  className?: string
  /** False: read only (a published post), no formatting bar. */
  editable?: boolean
  /** In a hidden tab: changes of `value` reach the document only once it shows again. */
  hidden?: boolean
  /** Under the text, above the bar (media thumbnails). */
  below?: React.ReactNode
  /** In the bottom bar before the formatting buttons (Upload). */
  start?: React.ReactNode
  /** In the text's bottom right corner (the counter), above the media, with or without the bar. */
  end?: React.ReactNode
}) {
  const last = useRef(value)
  // The first text only: Tiptap reads `content` once, and a new string per keystroke made it reset its options.
  const [initial] = useState(() => textToHtml(value))
  const editor = useEditor({
    immediatelyRender: false,
    shouldRerenderOnTransaction: false,
    extensions: [
      StarterKit.configure({
        heading: false,
        code: false,
        codeBlock: false,
        blockquote: false,
        horizontalRule: false,
        strike: false,
        underline: false,
        link: false,
      }),
      Link,
      Placeholder.configure({ placeholder: placeholder ?? '' }),
    ],
    content: initial,
    editorProps: {
      attributes: {
        id,
        role: 'textbox',
        'aria-multiline': 'true',
        ...(labelledBy && { 'aria-labelledby': labelledBy }),
        ...(invalid && { 'aria-invalid': 'true' }),
        ...(required && { 'aria-required': 'true' }),
        ...(testId && { 'data-testid': testId }),
      },
    },
    onUpdate: ({ editor: e }) => {
      last.current = docToText(e.getJSON())
      onChange(last.current)
    },
  })

  useEffect(() => {
    if (editor && !hidden && value !== last.current) {
      last.current = value
      editor.commands.setContent(textToHtml(value), { emitUpdate: false })
    }
  }, [editor, value, hidden])

  return (
    <RichTextEditorRoot editor={editor} editable={editable} variant="subtle" className={cn('post-editor', className)}>
      {/* The counter sits in the box's bottom right corner, under the text or beside the media: the text keeps room
          at its foot (no media) or the media row keeps room on its right, so it never covers either. */}
      <div className={cn('relative', end && '[&_.media-strip]:pr-36 [&:not(:has(.media-strip))_.ProseMirror]:pb-10')}>
        <Content />
        {below}
        {end && <div className="absolute right-2 bottom-2">{end}</div>}
      </div>
      {/* The tools sit in a bar under the text, not in a toolbar above it. */}
      <div
        hidden={!editable}
        className="flex flex-wrap items-center gap-1 border-t px-2 py-1.5"
        role="toolbar"
        aria-label="Formatting"
      >
        {start}
        <ControlsGroup>
          <BoldControl />
          <ItalicControl />
        </ControlsGroup>
        <ControlsGroup>
          <BulletListControl />
          <OrderedListControl />
        </ControlsGroup>
        <ControlsGroup>
          <LinkControl />
          <UnlinkControl />
        </ControlsGroup>
        <ControlsGroup>
          <UndoControl />
          <RedoControl />
        </ControlsGroup>
      </div>
    </RichTextEditorRoot>
  )
}
