/** @type {import('tailwindcss').Config} */
// Reprend à l'identique la config qui était inline dans index.html,
// du temps où la page chargeait cdn.tailwindcss.com.
module.exports = {
  content: ['./index.html'],
  theme: {
    extend: {
      colors: {
        primary: { DEFAULT: '#2D7A5F', dark: '#235f4a', light: '#3d9a7a' },
        secondary: { DEFAULT: '#D97706', dark: '#b86205' },
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'monospace'],
      },
    },
  },
  // Ajoutées par le JS (menu mobile), donc absentes d'une partie du markup.
  safelist: ['hidden', 'flex'],
}
