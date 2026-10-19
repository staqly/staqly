// The date-time picker as a person uses it: the day from the calendar, the
// hour and the minute from the clock, then Done.
import type { Locator, Page } from '@playwright/test'

import { expect } from './fixtures.js'

const pad = (n: number) => String(n).padStart(2, '0')
const dayName = (d: Date) => d.toLocaleDateString('en-US', { weekday: 'long', day: 'numeric', month: 'long' })

/**
 * Picks `when` in the date-time picker inside `within` (a sheet or popover): the day button by its name,
 * the hour and minute from the clock. Works whatever month the picker opened on.
 */
export async function pickDateTime(page: Page, within: Locator, when: Date): Promise<void> {
  await within.getByTestId('date-time-picker').click()
  const picker = page.getByRole('dialog', { name: 'Pick a date and time' })

  await expect(picker).toBeVisible()
  const shown = picker.getByRole('grid')
  // Move to the month of `when` if the calendar opened on another one.
  const target = when.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })

  for (let i = 0; i < 3 && !(await shown.getAttribute('aria-label'))?.startsWith(target.split(' ')[0]!); i++) {
    await picker.getByRole('button', { name: 'Next month' }).click()
  }

  await picker.getByRole('button', { name: dayName(when) }).click()
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
  await picker.getByRole('button', { name: 'Done' }).click()
  await expect(picker).toBeHidden()
}
