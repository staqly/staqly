import type { DragEvent } from 'react'

const SHAPES = {
  // A media file: its square thumbnail.
  tile: { chip: 'padding:4px 12px 4px 4px;border-radius:10px', img: 'width:28px;height:28px;border-radius:6px' },
  // A channel: its round profile picture.
  round: { chip: 'padding:6px 12px 6px 6px;border-radius:9999px', img: 'width:24px;height:24px;border-radius:9999px' },
}

/**
 * What follows the pointer while something is dragged: a small chip with the dragged element's first picture and
 * a label, instead of the browser's see-through copy of the whole row or tile. setDragImage needs the element in
 * the document while it takes its snapshot, so the chip is built off screen and removed on the next task.
 */
export function setDragChip(e: DragEvent, label: string, shape: keyof typeof SHAPES) {
  const s = SHAPES[shape]
  const chip = document.createElement('div')

  chip.style.cssText =
    `position:fixed;top:-1000px;left:-1000px;display:flex;align-items:center;gap:8px;max-width:260px;${s.chip};` +
    'background:var(--popover);color:var(--popover-foreground);border:1px solid var(--brand);' +
    'box-shadow:0 8px 24px rgb(0 0 0 / 0.35);font:500 13px/1.2 var(--font-sans, system-ui);white-space:nowrap'

  const picture = (e.currentTarget as HTMLElement).querySelector('img')

  if (picture) {
    const img = picture.cloneNode() as HTMLImageElement

    img.style.cssText = `${s.img};object-fit:cover`
    chip.append(img)
  }

  const text = document.createElement('span')

  text.style.cssText = 'overflow:hidden;text-overflow:ellipsis'
  text.textContent = label
  chip.append(text)
  document.body.append(chip)
  e.dataTransfer.setDragImage(chip, 16, 16)
  setTimeout(() => chip.remove())
}
