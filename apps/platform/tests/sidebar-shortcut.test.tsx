import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { SidebarProvider, useSidebar } from '@/components/ui/sidebar'

function State() {
  const { state } = useSidebar()

  return (
    <>
      <output data-testid="sidebar-state">{state}</output>
      <div contentEditable suppressContentEditableWarning data-testid="editor">
        Post text
      </div>
      <input aria-label="Name" />
    </>
  )
}

describe('sidebar shortcut', () => {
  it('⌘B toggles the sidebar, but not while typing in the post editor or a field', () => {
    render(
      <SidebarProvider>
        <State />
      </SidebarProvider>,
    )
    const state = screen.getByTestId('sidebar-state')

    expect(state).toHaveTextContent('expanded')
    // Bold in the editor: the sidebar stays.
    fireEvent.keyDown(screen.getByTestId('editor'), { key: 'b', metaKey: true })
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Name' }), { key: 'b', ctrlKey: true })
    expect(state).toHaveTextContent('expanded')
    fireEvent.keyDown(document.body, { key: 'b', metaKey: true })
    expect(state).toHaveTextContent('collapsed')
  })
})
