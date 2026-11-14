import { createControl } from './rte-control'

interface HistoryCanCommands {
  redo: () => boolean
  undo: () => boolean
}

export const BoldControl = createControl({
  iconKey: 'boldControlIcon',
  isActive: { name: 'bold' },
  label: 'boldControlLabel',
  operation: { name: 'toggleBold' },
})

export const ItalicControl = createControl({
  iconKey: 'italicControlIcon',
  isActive: { name: 'italic' },
  label: 'italicControlLabel',
  operation: { name: 'toggleItalic' },
})

export const BulletListControl = createControl({
  iconKey: 'bulletListControlIcon',
  isActive: { name: 'bulletList' },
  label: 'bulletListControlLabel',
  operation: { name: 'toggleBulletList' },
})

export const OrderedListControl = createControl({
  iconKey: 'orderedListControlIcon',
  isActive: { name: 'orderedList' },
  label: 'orderedListControlLabel',
  operation: { name: 'toggleOrderedList' },
})

export const UnlinkControl = createControl({
  iconKey: 'unlinkControlIcon',
  label: 'unlinkControlLabel',
  operation: { name: 'unsetLink' },
})

export const UndoControl = createControl({
  iconKey: 'undoControlIcon',
  isDisabled: (editor) => !(editor.can() as unknown as HistoryCanCommands).undo(),
  label: 'undoControlLabel',
  operation: { name: 'undo' },
})

export const RedoControl = createControl({
  iconKey: 'redoControlIcon',
  isDisabled: (editor) => !(editor.can() as unknown as HistoryCanCommands).redo(),
  label: 'redoControlLabel',
  operation: { name: 'redo' },
})
