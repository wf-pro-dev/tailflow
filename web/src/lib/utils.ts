// Simple cn() utility — Tailwind class joiner (no clsx/tailwind-merge dependency)
export function cn(...classes: (string | false | null | undefined)[]): string {
  return classes.filter(Boolean).join(" ");
}
