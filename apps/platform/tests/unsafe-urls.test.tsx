import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { Invoices } from '@/components/billing/payments'
import { UserAvatar } from '@/components/settings/user-avatar'
import type { BillingDetails } from '@/lib/api'

import { renderWithProviders } from './utils'

afterEach(cleanup)

// URLs from the API reach href/src only through safeHttpUrl: a javascript: link would run in the dashboard.
describe('URLs from the API', () => {
  it('an invoice with a javascript: link gets no link', () => {
    const details = {
      invoices: [
        {
          id: 'in_1',
          number: 'A-0001',
          created: '2026-10-15T10:00:00Z',
          period_start: null,
          period_end: null,
          total: 3000,
          amount_paid: 3000,
          amount_due: 0,
          currency: 'usd',
          status: 'paid',
          hosted_invoice_url: 'javascript:alert(1)',
          invoice_pdf: 'https://invoice.stripe.test/1.pdf',
        },
      ],
      has_more_invoices: false,
    } as unknown as BillingDetails

    renderWithProviders(<Invoices details={details} />)

    expect(screen.queryByRole('link', { name: /view/i })).toBeNull()
    expect(screen.getByRole('link', { name: /pdf of a-0001/i })).toHaveAttribute(
      'href',
      'https://invoice.stripe.test/1.pdf',
    )
  })

  it('a picture that is not http(s) shows the initials', () => {
    const { container } = render(<UserAvatar name="Ada Lovelace" src="javascript:alert(1)" />)

    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('[data-initials="AL"]')).toBeInTheDocument()
  })
})
