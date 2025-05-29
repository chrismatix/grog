import { colors, fontSizes, spacing, breakpoints, theme } from '../src/theme';

describe('Theme Configuration', () => {
  describe('colors', () => {
    test('has all required color palettes', () => {
      expect(colors).toHaveProperty('primary');
      expect(colors).toHaveProperty('secondary');
      expect(colors).toHaveProperty('success');
      expect(colors).toHaveProperty('error');
    });

    test('primary palette has all required shades', () => {
      expect(colors.primary).toHaveProperty('50');
      expect(colors.primary).toHaveProperty('100');
      expect(colors.primary).toHaveProperty('200');
      expect(colors.primary).toHaveProperty('300');
      expect(colors.primary).toHaveProperty('400');
      expect(colors.primary).toHaveProperty('500');
      expect(colors.primary).toHaveProperty('600');
      expect(colors.primary).toHaveProperty('700');
      expect(colors.primary).toHaveProperty('800');
      expect(colors.primary).toHaveProperty('900');
      expect(colors.primary).toHaveProperty('950');
    });

    test('color values are valid hex colors', () => {
      const hexColorRegex = /^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$/;

      // Test a sample of colors
      expect(hexColorRegex.test(colors.primary[500])).toBe(true);
      expect(hexColorRegex.test(colors.secondary[500])).toBe(true);
      expect(hexColorRegex.test(colors.success[500])).toBe(true);
      expect(hexColorRegex.test(colors.error[500])).toBe(true);
    });
  });

  describe('fontSizes', () => {
    test('has all required font sizes', () => {
      expect(fontSizes).toHaveProperty('xs');
      expect(fontSizes).toHaveProperty('sm');
      expect(fontSizes).toHaveProperty('base');
      expect(fontSizes).toHaveProperty('lg');
      expect(fontSizes).toHaveProperty('xl');
      expect(fontSizes).toHaveProperty('2xl');
      expect(fontSizes).toHaveProperty('3xl');
      expect(fontSizes).toHaveProperty('4xl');
    });

    test('font size values are valid CSS units', () => {
      const cssUnitRegex = /^(\d*\.?\d+)(rem|em|px|%)$/;

      // Test a sample of font sizes
      expect(cssUnitRegex.test(fontSizes.base)).toBe(true);
      expect(cssUnitRegex.test(fontSizes.lg)).toBe(true);
      expect(cssUnitRegex.test(fontSizes.xl)).toBe(true);
    });
  });

  describe('spacing', () => {
    test('has all required spacing values', () => {
      expect(spacing).toHaveProperty('0');
      expect(spacing).toHaveProperty('1');
      expect(spacing).toHaveProperty('2');
      expect(spacing).toHaveProperty('4');
      expect(spacing).toHaveProperty('8');
      expect(spacing).toHaveProperty('16');
    });

    test('spacing values are valid CSS units', () => {
      const cssUnitRegex = /^(\d*\.?\d+)(rem|em|px|%)$|^0$/;

      // Test a sample of spacing values
      expect(cssUnitRegex.test(spacing[0])).toBe(true);
      expect(cssUnitRegex.test(spacing[4])).toBe(true);
      expect(cssUnitRegex.test(spacing[8])).toBe(true);
    });
  });

  describe('breakpoints', () => {
    test('has all required breakpoints', () => {
      expect(breakpoints).toHaveProperty('sm');
      expect(breakpoints).toHaveProperty('md');
      expect(breakpoints).toHaveProperty('lg');
      expect(breakpoints).toHaveProperty('xl');
      expect(breakpoints).toHaveProperty('2xl');
    });

    test('breakpoint values are valid CSS units', () => {
      const cssUnitRegex = /^(\d*\.?\d+)(rem|em|px|%)$/;

      // Test all breakpoints
      expect(cssUnitRegex.test(breakpoints.sm)).toBe(true);
      expect(cssUnitRegex.test(breakpoints.md)).toBe(true);
      expect(cssUnitRegex.test(breakpoints.lg)).toBe(true);
      expect(cssUnitRegex.test(breakpoints.xl)).toBe(true);
      expect(cssUnitRegex.test(breakpoints['2xl'])).toBe(true);
    });
  });

  describe('theme object', () => {
    test('combines all theme parts', () => {
      expect(theme.colors).toBe(colors);
      expect(theme.fontSizes).toBe(fontSizes);
      expect(theme.spacing).toBe(spacing);
      expect(theme.breakpoints).toBe(breakpoints);
    });
  });
});
