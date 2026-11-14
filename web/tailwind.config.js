/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: ["class", '[data-theme="dark"]'],
  theme: {
    extend: {
      colors: {
        /*
         * Semantic token colors. Each resolves to a CSS variable defined in
         * styles.css under [data-theme="dark"] / [data-theme="light"], so a
         * single class (e.g. bg-surface) is correct in both themes.
         *
         * RGB-channel form (rgb(var(--x-rgb) / <alpha-value>)) is used for
         * colors that need opacity modifiers (bg-accent/10, bg-status-offline/10).
         */
        surface: {
          base: "var(--bg-base)",
          DEFAULT: "var(--bg-surface)",
          raised: "var(--bg-raised)",
          overlay: "var(--bg-overlay)",
          sunken: "var(--bg-sunken)",
        },
        line: {
          subtle: "var(--line-subtle)",
          DEFAULT: "var(--line)",
          strong: "var(--line-strong)",
        },
        ink: {
          primary: "rgb(var(--ink-primary-rgb) / <alpha-value>)",
          secondary: "rgb(var(--ink-secondary-rgb) / <alpha-value>)",
          muted: "rgb(var(--ink-muted-rgb) / <alpha-value>)",
          faint: "rgb(var(--ink-faint-rgb) / <alpha-value>)",
        },
        accent: {
          DEFAULT: "rgb(var(--accent-rgb) / <alpha-value>)",
          hover: "var(--accent-hover)",
          contrast: "var(--accent-contrast)",
        },
        status: {
          healthy: "rgb(var(--status-healthy-rgb) / <alpha-value>)",
          degraded: "rgb(var(--status-degraded-rgb) / <alpha-value>)",
          stale: "rgb(var(--status-stale-rgb) / <alpha-value>)",
          offline: "rgb(var(--status-offline-rgb) / <alpha-value>)",
          noagent: "rgb(var(--status-noagent-rgb) / <alpha-value>)",
        },
        danger: "rgb(var(--danger-rgb) / <alpha-value>)",
        /*
         * Remap the zinc text shades onto the ink tokens so the ~150 existing
         * text-zinc-* usages become theme-aware WITHOUT a per-file sweep.
         *   zinc-50/100/200 -> ink-primary   (bright headings/values)
         *   zinc-300/400    -> ink-secondary (body, labels)
         *   zinc-500        -> ink-muted     (captions, subtext)
         *   zinc-600        -> ink-faint     (disabled, dividers text)
         * Shades 700-950 are left as Tailwind defaults (unused in this app).
         */
        zinc: {
          50: "rgb(var(--ink-primary-rgb) / <alpha-value>)",
          100: "rgb(var(--ink-primary-rgb) / <alpha-value>)",
          200: "rgb(var(--ink-primary-rgb) / <alpha-value>)",
          300: "rgb(var(--ink-secondary-rgb) / <alpha-value>)",
          400: "rgb(var(--ink-secondary-rgb) / <alpha-value>)",
          500: "rgb(var(--ink-muted-rgb) / <alpha-value>)",
          600: "rgb(var(--ink-faint-rgb) / <alpha-value>)",
          700: "#3f3f46",
          800: "#27272a",
          900: "#18181b",
          950: "#09090b",
        },
        // Chart palette — colorblind-safe perceptual sequence (Okabe-Ito derived).
        // Index 1 is the primary series and uses the accent.
        chart: {
          1: "var(--accent)",
          2: "#E8A33D",
          3: "#3ECF8E",
          4: "#A88AE0",
          5: "#6BAFD6",
        },
      },
      fontFamily: {
        sans: ["Inter", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "system-ui", "sans-serif"],
        mono: ["JetBrains Mono", "SF Mono", "Menlo", "Monaco", "Consolas", "monospace"],
      },
      boxShadow: {
        overlay: "var(--shadow-overlay)",
        pop: "var(--shadow-pop)",
      },
      animation: {
        "fade-in": "fade-in 0.2s ease-out",
        "slide-in-right": "slide-in-right 0.3s ease-out",
        "number-tick": "number-tick 0.15s ease-out",
      },
      keyframes: {
        // live-breath / skeleton-shimmer / fade-in / slide-in-right / number-tick
        // are defined in styles.css @layer components.
      },
    },
  },
  plugins: [],
};
