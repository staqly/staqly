import { Bold, Italic, Link, Link2Off, List, ListOrdered, Redo2, Undo2 } from 'lucide-react'

import type { RichTextEditorIcons } from './icon-types'

const iconProps = { className: 'rte-editor-icon' }

export const DEFAULT_ICONS: RichTextEditorIcons = {
  boldControlIcon: <Bold {...iconProps} />,
  bulletListControlIcon: <List {...iconProps} />,
  italicControlIcon: <Italic {...iconProps} />,
  linkControlIcon: <Link {...iconProps} />,
  orderedListControlIcon: <ListOrdered {...iconProps} />,
  redoControlIcon: <Redo2 {...iconProps} />,
  undoControlIcon: <Undo2 {...iconProps} />,
  unlinkControlIcon: <Link2Off {...iconProps} />,
}
