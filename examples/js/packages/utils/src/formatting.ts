/**
 * Formats a number as a currency string
 * @param value The number to format
 * @param currency The currency code (default: USD)
 * @param locale The locale to use for formatting (default: en-US)
 * @returns Formatted currency string
 */
export function formatCurrency(
  value: number,
  currency: string = 'USD',
  locale: string = 'en-US'
): string {
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency,
  }).format(value);
}

/**
 * Formats a date as a string
 * @param date The date to format
 * @param locale The locale to use for formatting (default: en-US)
 * @param options Intl.DateTimeFormatOptions
 * @returns Formatted date string
 */
export function formatDate(
  date: Date | number,
  locale: string = 'en-US',
  options: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: 'long',
    day: 'numeric'
  }
): string {
  return new Intl.DateTimeFormat(locale, options).format(date);
}
