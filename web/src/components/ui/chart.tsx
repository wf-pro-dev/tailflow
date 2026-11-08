import * as React from "react";
import * as RechartsPrimitive from "recharts";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart,
  XAxis, YAxis, type TooltipProps,
} from "recharts";
import { cn } from "@/lib/utils";

// --- shadcn/ui ChartContainer (wraps ResponsiveContainer with theming) ---
const THEMES = { light: "", dark: ".dark" } as const;

export type ChartConfig = {
  [k in string]: {
    label?: React.ReactNode;
    icon?: React.ReactNode;
  } & ({ color?: string; theme?: never } | { color?: never; theme: Record<keyof typeof THEMES, string> })
};

type ChartContextProps = {
  config: ChartConfig;
};

const ChartContext = React.createContext<ChartContextProps | null>(null);

function useChart() {
  const context = React.useContext(ChartContext);
  if (!context) {
    throw new Error("useChart must be used within a <ChartContainer />");
  }
  return context;
}

export function ChartContainer({
  config,
  className,
  children,
  ...props
}: React.ComponentProps<"div"> & { config: ChartConfig; children: React.ComponentProps<typeof RechartsPrimitive.ResponsiveContainer>["children"]; }) {
  return (
    <ChartContext.Provider value={{ config }}>
      <div className={cn("w-full", className)} {...props}>
        <RechartsPrimitive.ResponsiveContainer>{children}</RechartsPrimitive.ResponsiveContainer>
      </div>
    </ChartContext.Provider>
  );
}

export function ChartTooltip({ active, payload, label }: TooltipProps<number, string>) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-lg border border-white/[0.08] bg-[#18181B] px-3 py-2 text-xs shadow-xl">
      {label && <div className="mb-1 text-zinc-500">{label}</div>}
      <div className="space-y-1">
        {payload.map((item, i) => (
          <div key={i} className="flex items-center gap-2">
            <span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} />
            <span className="text-zinc-400">{item.name}</span>
            <span className="font-mono text-zinc-200">{typeof item.value === "number" ? item.value.toFixed(1) : item.value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// Re-export recharts components for convenience
export { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, XAxis, YAxis };
