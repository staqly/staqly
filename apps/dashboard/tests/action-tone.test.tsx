import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ActionIcon, actionTone } from '@/components/items/item-actions'
import type { ActionDef } from '@/lib/api'

const action = (name: string, set_status?: ActionDef['set_status']): ActionDef => ({
  name,
  label: name,
  event: `item.${name}`,
  requires_feedback: false,
  set_status,
})

const iconClass = (a: ActionDef) =>
  render(<ActionIcon action={a} />)
    .container.querySelector('svg')!
    .getAttribute('class')

describe('custom action buttons', () => {
  it('colour by the status they set, all in one style', () => {
    expect(actionTone(action('archive', 'archived')).className).toContain('orange')
    expect(actionTone(action('fail', 'failed')).className).toContain('red')
    expect(actionTone(action('publish', 'processing')).className).toContain('blue')
    // Same style, only the colour differs.
    expect(actionTone(action('archive', 'archived')).variant).toBe(actionTone(action('publish', 'processing')).variant)
    expect(actionTone(action('ping')).variant).toBe('outline')
  })

  it('give publish and archive different icons', () => {
    expect(iconClass(action('publish', 'processing'))).not.toBe(iconClass(action('archive', 'archived')))
    expect(iconClass(action('custom', 'archived'))).toBe(iconClass(action('archive', 'archived')))
  })
})
