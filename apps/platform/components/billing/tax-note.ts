/**
 * The tax a price includes, by its currency: CHF is what Switzerland and Liechtenstein pay (Swiss VAT), EUR the EU
 * (VAT), USD everyone else (whatever sales tax applies). Prices are tax-inclusive in every currency.
 */
export const taxNote = (currency = 'eur') =>
  ({ chf: 'incl. Swiss VAT', eur: 'incl. VAT' })[currency] ?? 'incl. applicable taxes'
