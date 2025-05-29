import { formatCurrency, formatDate } from '../src/formatting';

describe('Formatting Utilities', () => {
  describe('formatCurrency', () => {
    test('formats with default parameters (USD, en-US)', () => {
      expect(formatCurrency(1000)).toBe('$1,000.00');
      expect(formatCurrency(1234.56)).toBe('$1,234.56');
      expect(formatCurrency(0)).toBe('$0.00');
    });

    test('formats with custom currency', () => {
      expect(formatCurrency(1000, 'EUR')).toBe('€1,000.00');
      expect(formatCurrency(1000, 'JPY')).toBe('¥1,000');
      expect(formatCurrency(1000, 'GBP')).toBe('£1,000.00');
    });

    test('formats with custom locale', () => {
      expect(formatCurrency(1000, 'USD', 'de-DE')).toMatch(/1\.000,00/);
      expect(formatCurrency(1000, 'EUR', 'de-DE')).toMatch(/1\.000,00/);
    });
  });

  describe('formatDate', () => {
    // Use a fixed date for consistent testing
    const testDate = new Date(2023, 0, 15); // January 15, 2023

    test('formats with default parameters', () => {
      expect(formatDate(testDate)).toBe('January 15, 2023');
    });

    test('formats with custom locale', () => {
      expect(formatDate(testDate, 'de-DE')).toBe('15. Januar 2023');
      expect(formatDate(testDate, 'fr-FR')).toBe('15 janvier 2023');
    });

    test('formats with custom options', () => {
      expect(formatDate(testDate, 'en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric'
      })).toBe('Jan 15, 2023');

      expect(formatDate(testDate, 'en-US', {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric'
      })).toBe('Sunday, January 15, 2023');
    });

    test('accepts timestamp as input', () => {
      const timestamp = testDate.getTime();
      expect(formatDate(timestamp)).toBe('January 15, 2023');
    });
  });
});
