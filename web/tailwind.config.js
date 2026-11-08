/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        // Dark Aurora palette — Linear quiet dark + neon accents
        status: {
          healthy: "#22C55E",
          degraded: "#F59E0B",
          stale: "#84CC16",
          offline: "#71717A",
          noagent: "#A855F7",
        },
        accent: {
          DEFAULT: "#5E6AD2",
          hover: "#7B85E0",
        },
        neon: {
          cyan: "#00FFF1",
          violet: "#9c40ff",
          amber: "#ffaa40",
        },
        chart: {
          1: "#3DA5E0",
          2: "#3B5BDB",
          3: "#6F3FE0",
          4: "#8B30E0",
        },
      },
      fontFamily: {
        sans: ["Inter", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "SF Mono", "Menlo", "Monaco", "Consolas", "monospace"],
      },
      animation: {
        "pulse-glow": "pulse-glow 2s ease-in-out infinite",
        "fade-in": "fade-in 0.2s ease-out",
        "slide-in-right": "slide-in-right 0.3s ease-out",
        "number-tick": "number-tick 0.5s ease-out",
      },
      keyframes: {
        "pulse-glow": {
          "0%, 100%": { opacity: "1", boxShadow: "0 0 0 0 rgba(34, 197, 94, 0.4)" },
          "50%": { opacity: "0.8", boxShadow: "0 0 0 6px rgba(34, 197, 94, 0)" },
        },
        "fade-in": {
          "0%": { opacity: "0" },
          "100%": { opacity: "1" },
        },
        "slide-in-right": {
          "0%": { transform: "translateX(100%)" },
          "100%": { transform: "translateX(0)" },
        },
      },
    },
  },
  plugins: [],
};
