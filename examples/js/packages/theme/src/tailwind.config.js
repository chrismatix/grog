const { colors, fontSizes, spacing, breakpoints } = require('./theme');

/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    // This will be extended by consuming projects
  ],
  theme: {
    extend: {
      colors,
      fontSize: fontSizes,
      spacing,
      screens: {
        sm: breakpoints.sm,
        md: breakpoints.md,
        lg: breakpoints.lg,
        xl: breakpoints.xl,
        '2xl': breakpoints['2xl'],
      },
      fontFamily: {
        sans: ['var(--font-geist-sans)'],
        mono: ['var(--font-geist-mono)'],
      },
    },
  },
  plugins: [],
};
