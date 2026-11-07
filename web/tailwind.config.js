/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Design §17.7: status palette with 4.5:1 contrast against both backgrounds.
        status: {
          healthy: "#16a34a",
          degraded: "#d97706",
          stale: "#65a30d",
          offline: "#6b7280",
          noagent: "#9333ea",
        },
      },
    },
  },
  plugins: [],
};
