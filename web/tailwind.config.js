/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#e6fff2',
          100: '#b3ffdb',
          200: '#80ffc4',
          300: '#4dffad',
          400: '#1aff96',
          500: '#00ff88',
          600: '#00cc6a',
          700: '#00994f',
          800: '#006635',
          900: '#00331a',
          950: '#001a0d',
        },
        secondary: {
          50: '#e6fff2',
          100: '#b3ffdb',
          200: '#80ffc4',
          300: '#4dffad',
          400: '#1aff96',
          500: '#00ff88',
          600: '#00cc6a',
          700: '#15803d',
          800: '#166534',
          900: '#14532d',
          950: '#052e16',
        },
        monitor: {
          bg: '#0a0f1a',
          surface: '#0d1525',
          card: '#111b2e',
          border: 'rgba(0,255,136,0.12)',
          'border-bright': 'rgba(0,255,136,0.25)',
          green: '#00ff88',
          'green-dim': '#00cc6a',
          'green-muted': 'rgba(0,255,136,0.4)',
          'green-subtle': 'rgba(0,255,136,0.08)',
          'green-glow': 'rgba(0,255,136,0.15)',
          text: 'rgba(0,255,136,0.85)',
          'text-dim': 'rgba(0,255,136,0.5)',
          'text-muted': 'rgba(0,255,136,0.3)',
          red: '#ff4444',
          amber: '#ffaa00',
          cyan: '#00ddff',
        },
      },
      boxShadow: {
        'monitor': '0 0 15px rgba(0,255,136,0.05)',
        'monitor-lg': '0 0 30px rgba(0,255,136,0.08)',
        'glow': '0 0 10px rgba(0,255,136,0.2)',
      },
    },
  },
  plugins: [
    require('@tailwindcss/forms'),
  ],
}

