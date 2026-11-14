import { createContext, useContext, useState, useCallback, type ReactNode } from "react";

/*
 * Density toggle — compact vs comfortable. Affects row padding, sparkline
 * size, and how many rows fit in the viewport. Persisted to localStorage.
 * Ops people want both modes; the toggle lives in the topbar and Settings.
 */

export type Density = "compact" | "comfortable";

const STORAGE_KEY = "tailflow.density";
const DEFAULT: Density = "comfortable";

function readStored(): Density {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === "compact" || v === "comfortable") return v;
  } catch {
    /* ignore */
  }
  return DEFAULT;
}

interface DensityCtx {
  density: Density;
  setDensity: (d: Density) => void;
  toggle: () => void;
  /** Tailwind row padding classes for the current density. */
  rowPad: string;
  /** Cell horizontal padding. */
  cellPad: string;
  /** Sparkline height in px. */
  sparkH: number;
}

const Ctx = createContext<DensityCtx | null>(null);

export function DensityProvider({ children }: { children: ReactNode }) {
  const [density, setDensityState] = useState<Density>(() =>
    typeof window === "undefined" ? DEFAULT : readStored(),
  );

  const setDensity = useCallback((d: Density) => {
    try {
      localStorage.setItem(STORAGE_KEY, d);
    } catch {
      /* ignore */
    }
    setDensityState(d);
  }, []);

  const toggle = useCallback(() => {
    setDensity(density === "compact" ? "comfortable" : "compact");
  }, [density, setDensity]);

  const value: DensityCtx = {
    density,
    setDensity,
    toggle,
    rowPad: density === "compact" ? "py-1.5" : "py-3",
    cellPad: density === "compact" ? "px-3" : "px-4",
    sparkH: density === "compact" ? 22 : 32,
  };

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useDensity(): DensityCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useDensity must be used within DensityProvider");
  return ctx;
}
