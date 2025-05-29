import { isValidEmail, isValidUrl, isNotEmpty } from '../src/validation';

describe('Validation Utilities', () => {
  describe('isValidEmail', () => {
    test('returns true for valid email addresses', () => {
      expect(isValidEmail('user@example.com')).toBe(true);
      expect(isValidEmail('user.name@example.co.uk')).toBe(true);
      expect(isValidEmail('user+tag@example.com')).toBe(true);
      expect(isValidEmail('user123@example.com')).toBe(true);
    });

    test('returns false for invalid email addresses', () => {
      expect(isValidEmail('')).toBe(false);
      expect(isValidEmail('user')).toBe(false);
      expect(isValidEmail('user@')).toBe(false);
      expect(isValidEmail('@example.com')).toBe(false);
      expect(isValidEmail('user@example')).toBe(false);
      expect(isValidEmail('user@.com')).toBe(false);
      expect(isValidEmail('user@example.')).toBe(false);
      expect(isValidEmail('user@exam ple.com')).toBe(false);
    });
  });

  describe('isValidUrl', () => {
    test('returns true for valid URLs', () => {
      expect(isValidUrl('https://example.com')).toBe(true);
      expect(isValidUrl('http://example.com')).toBe(true);
      expect(isValidUrl('https://www.example.com/path')).toBe(true);
      expect(isValidUrl('https://example.com/path?query=value')).toBe(true);
      expect(isValidUrl('https://example.com:8080')).toBe(true);
    });

    test('returns false for invalid URLs', () => {
      expect(isValidUrl('')).toBe(false);
      expect(isValidUrl('example')).toBe(false);
      expect(isValidUrl('example.com')).toBe(false); // Missing protocol
    });
  });

  describe('isNotEmpty', () => {
    test('returns true for non-empty strings', () => {
      expect(isNotEmpty('hello')).toBe(true);
      expect(isNotEmpty('  hello  ')).toBe(true);
      expect(isNotEmpty('123')).toBe(true);
      expect(isNotEmpty(' ')).toBe(false);
    });

    test('returns false for empty strings', () => {
      expect(isNotEmpty('')).toBe(false);
      expect(isNotEmpty('  ')).toBe(false);
      expect(isNotEmpty('\t')).toBe(false);
      expect(isNotEmpty('\n')).toBe(false);
    });
  });
});
