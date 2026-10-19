import TipTapLink from '@tiptap/extension-link'

/** Mod-K asks the link control of this editor (event.detail) to open; a page may hold several editors. */
export const EDIT_LINK_EVENT = 'edit-link'

export const Link = TipTapLink.extend({
  addKeyboardShortcuts() {
    return {
      'Mod-k': () => {
        window.dispatchEvent(new CustomEvent(EDIT_LINK_EVENT, { detail: this.editor }))

        // The shell's Mod-K (the command palette) leaves rich text editors alone.
        return true
      },
    }
  },
}).configure({ openOnClick: false })
