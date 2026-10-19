export interface RichTextEditorLabels {
  boldControlLabel: string
  italicControlLabel: string
  bulletListControlLabel: string
  orderedListControlLabel: string
  linkControlLabel: string
  unlinkControlLabel: string
  undoControlLabel: string
  redoControlLabel: string
  // link editor
  linkEditorInputLabel: string
  linkEditorInputPlaceholder: string
  linkEditorSave: string
}

export const DEFAULT_LABELS: RichTextEditorLabels = {
  boldControlLabel: 'Bold',
  bulletListControlLabel: 'Bullet list',
  italicControlLabel: 'Italic',
  linkControlLabel: 'Link',
  linkEditorInputLabel: 'Enter URL',
  linkEditorInputPlaceholder: 'https://example.com',
  linkEditorSave: 'Save',
  orderedListControlLabel: 'Ordered list',
  redoControlLabel: 'Redo',
  undoControlLabel: 'Undo',
  unlinkControlLabel: 'Remove link',
}
