/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}'],
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#f0faf5',
          DEFAULT: '#2D7A5F',
          dark: '#235f4a',
          light: '#3d9a7a'
        },
        secondary: {
          50: '#fff8ed',
          DEFAULT: '#D97706',
          dark: '#b86205'
        }
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'monospace']
      }
    }
  },
  plugins: []
};
