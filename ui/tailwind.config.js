/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      // Semantic colors resolve to the tokens in src/styles.css, which switch
      // with the theme, so components need no dark: variants for color.
      colors: {
        canvas: 'var(--ns-canvas)',
        surface: {
          DEFAULT: 'var(--ns-surface)',
          muted: 'var(--ns-surface-muted)',
          hover: 'var(--ns-surface-hover)',
          selected: 'var(--ns-surface-selected)',
        },
        line: {
          DEFAULT: 'var(--ns-border)',
          muted: 'var(--ns-border-muted)',
        },
        fg: {
          DEFAULT: 'var(--ns-fg)',
          muted: 'var(--ns-fg-muted)',
          subtle: 'var(--ns-fg-subtle)',
          onEmphasis: 'var(--ns-on-emphasis)',
        },
        accent: {
          DEFAULT: 'var(--ns-accent)',
          emphasis: 'var(--ns-accent-emphasis)',
          muted: 'var(--ns-accent-muted)',
        },
        success: { DEFAULT: 'var(--ns-success)', muted: 'var(--ns-success-muted)' },
        attention: { DEFAULT: 'var(--ns-attention)', muted: 'var(--ns-attention-muted)' },
        danger: { DEFAULT: 'var(--ns-danger)', muted: 'var(--ns-danger-muted)' },
      },
      boxShadow: {
        card: 'var(--ns-shadow)',
        overlay: 'var(--ns-shadow-overlay)',
      },
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          '"Segoe UI"',
          '"Noto Sans"',
          'Helvetica',
          'Arial',
          'sans-serif',
        ],
        mono: ['ui-monospace', 'SFMono-Regular', '"SF Mono"', 'Menlo', 'Consolas', '"Liberation Mono"', 'monospace'],
      },
    },
  },
  plugins: [],
};
