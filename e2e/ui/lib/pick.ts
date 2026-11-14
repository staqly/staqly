// The date-time picker as a person uses it: the day from the calendar, the
// hour and the minute from the clock, then Done.
import type { Locator, Page } from '@playwright/test'

import { expect } from './fixtures.js'

const pad = (n: number) => String(n).padStart(2, '0')
const dayName = (d: Date) => d.toLocaleDateString('en-US', { weekday: 'long', day: 'numeric', month: 'long' })
/** "November 2026", as the calendar names its grid. */
const monthLabel = (d: Date) => d.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })
/** Months since year 0 of a grid's "November 2026", to know which way to step. */
const monthIndex = (label: string) => {
  const [month, year] = label.split(' ')
  const d = new Date(`${month} 1, ${year}`)

  return d.getFullYear() * 12 + d.getMonth()
}

/**
 * Picks `when` in the date-time picker inside `within` (a sheet or popover): the day button by its name,
 * the hour and minute from the clock, then `confirm` (the composer's popover says Schedule and sends the post).
 * Works whatever month the picker opened on.
 */
export async function pickDateTime(
  page: Page,
  within: Locator,
  when: Date,
  confirm: 'Done' | 'Schedule' = 'Done',
): Promise<void> {
  await within.getByTestId('date-time-picker').click()
  const picker = page.getByRole('dialog', { name: 'Pick a date and time' })

  await expect(picker).toBeVisible()
  const shown = picker.getByRole('grid')
  // Move to the month of `when` if the calendar opened on another one (the value's month, or today's): the grid
  // also shows the neighbouring months' first and last days under the same names, so the month must match.
  const target = monthLabel(when)

  for (let i = 0; i < 24; i++) {
    const label = (await shown.getAttribute('aria-label'))!

    if (label === target) {
      break
    }

    await picker
      .getByRole('button', { name: monthIndex(label) > monthIndex(target) ? 'Previous month' : 'Next month' })
      .click()
    // Read the grid again only once the step is on screen, or a slow render counts one step twice.
    await expect(shown).not.toHaveAttribute('aria-label', label)
  }

  await expect(shown).toHaveAttribute('aria-label', target)

  await picker.getByRole('button', { name: dayName(when), exact: true }).click()
  await picker
    .getByRole('radiogroup', { name: 'Hour' })
    .getByRole('radio', { name: pad(when.getHours()), exact: true })
    .click()
  await picker
    .getByRole('radiogroup', { name: 'Minute' })
    .getByRole('radio', { name: pad(when.getMinutes()), exact: true })
    .click()
  await expect(picker.getByTestId('date-time-summary')).toContainText(
    `, ${pad(when.getHours())}:${pad(when.getMinutes())}`,
  )
  await picker.getByRole('button', { name: confirm, exact: true }).click()
  await expect(picker).toBeHidden()
}

const escapeRe = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** The option for a channel in the composer's open Channels list (its name, then platform and handle). */
export const channelOption = (page: Page, name: string) =>
  page.getByRole('option', { name: new RegExp(`^${escapeRe(name)}`) })

/** Opens the composer's Channels multi-select in `sheet`, picks these channels and closes the list. */
export async function pickChannels(page: Page, sheet: Locator, ...names: string[]): Promise<void> {
  await sheet.getByRole('button', { name: 'Channels' }).click()

  for (const name of names) {
    const option = channelOption(page, name)

    await option.click()
    await expect(option).toHaveAttribute('data-checked', 'true')
  }

  await page.keyboard.press('Escape')
  await expect(page.getByRole('listbox')).toHaveCount(0)
}
