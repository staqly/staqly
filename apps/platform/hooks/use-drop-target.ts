'use client'

import { useState } from 'react'

/**
 * A drop target for rows dragged with drag data of `type` (a channel onto a group, posts onto a collection). A mouse
 * extra: keyboards and touch use the row's or the selection's Move.
 */
export function useDropTarget(type: string, onDrop: (data: string) => void) {
  const [over, setOver] = useState(false)

  return {
    over,
    props: {
      onDragOver: (e: React.DragEvent) => {
        if (e.dataTransfer.types.includes(type)) {
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
          setOver(true)
        }
      },
      onDragLeave: (e: React.DragEvent) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
          setOver(false)
        }
      },
      onDrop: (e: React.DragEvent) => {
        const data = e.dataTransfer.getData(type)

        setOver(false)

        if (data) {
          e.preventDefault()
          onDrop(data)
        }
      },
    },
  }
}
