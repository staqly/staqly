'use client'

import { Placeholder } from '@tiptap/extension-placeholder'
import { useEditor } from '@tiptap/react'
import { StarterKit } from '@tiptap/starter-kit'
import { useEffect, useRef } from 'react'

import { Link, RichTextEditor } from '@/components/editor'

import '@/components/editor/style.css'
import { docToText, textToHtml } from '@/lib/post-text'
import { cn } from '@/lib/utils'

/**
 * The post text field: editorcn's toolbar editor (Tiptap) over plain text. Bold and italic become Unicode
 * letters, lists become "•" or "1." lines (lib/post-text). `value` stays the source of truth: a change from
 * outside (another post, a reset) replaces the document.
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
  /** Under the text, above the bar (media thumbnails). */
  below?: React.ReactNode
  /** In the bottom bar before the formatting buttons (Insert media). */
  start?: React.ReactNode
  /** At the bar's right end (the counter). */
  end?: React.ReactNode
}) {
  const last = useRef(value)
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
    content: textToHtml(value),
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
    if (editor && value !== last.current) {
      last.current = value
      editor.commands.setContent(textToHtml(value), { emitUpdate: false })
    }
  }, [editor, value])

  return (
    <RichTextEditor editor={editor} editable={editable} variant="subtle" className={cn('post-editor', className)}>
      <RichTextEditor.Content />
      {below}
      {/* The tools sit in a bar under the text, not in a toolbar above it. */}
      <div
        hidden={!editable}
        className="flex flex-wrap items-center gap-1 border-t px-2 py-1.5"
        role="toolbar"
        aria-label="Formatting"
      >
        {start}
        <RichTextEditor.ControlsGroup>
          <RichTextEditor.Bold />
          <RichTextEditor.Italic />
        </RichTextEditor.ControlsGroup>
        <RichTextEditor.ControlsGroup>
          <RichTextEditor.BulletList />
          <RichTextEditor.OrderedList />
        </RichTextEditor.ControlsGroup>
        <RichTextEditor.ControlsGroup>
          <RichTextEditor.Link />
          <RichTextEditor.Unlink />
        </RichTextEditor.ControlsGroup>
        <RichTextEditor.ControlsGroup>
          <RichTextEditor.Undo />
          <RichTextEditor.Redo />
        </RichTextEditor.ControlsGroup>
        <span className="ml-auto">{end}</span>
      </div>
    </RichTextEditor>
  )
}
