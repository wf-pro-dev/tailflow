import { useEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";

interface Series {
  label: string;
  data: (number | null)[];
  color: string;
}

interface Props {
  t: number[];
  series: Series[];
  format?: (v: number) => string;
  yMax?: number;
  height?: number;
}

export function MetricChart({ t, series, format, yMax, height = 200 }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);

  useEffect(() => {
    if (!ref.current) return;

    // Build the data array: [timestamps, series1, series2, ...]
    const data: (number | null)[][] = [t];
    for (const s of series) {
      data.push(s.data);
    }

    const opts: uPlot.Options = {
      width: ref.current.clientWidth,
      height,
      series: [
        {},
        ...series.map((s) => ({
          label: s.label,
          stroke: s.color,
          width: 1.5,
          points: { show: false },
        })),
      ],
      axes: [
        {},
        {
          size: 60,
          values: (_, ticks) => ticks.map((v) => (format ? format(v) : String(v))),
          ...(yMax ? { range: [0, yMax] } : {}),
        },
      ],
      scales: {
        x: { time: true },
        y: { ...(yMax ? { range: [0, yMax] } : {}) },
      },
      cursor: {
        drag: { x: true, y: false },
      },
      legend: {
        show: true,
        live: true,
      },
    };

    plotRef.current = new uPlot(opts, data as any, ref.current);

    const onResize = () => {
      if (plotRef.current && ref.current) {
        plotRef.current.setSize({ width: ref.current.clientWidth, height });
      }
    };
    window.addEventListener("resize", onResize);

    return () => {
      window.removeEventListener("resize", onResize);
      plotRef.current?.destroy();
      plotRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Update data when it changes.
  useEffect(() => {
    if (!plotRef.current) return;
    const data: (number | null)[][] = [t];
    for (const s of series) {
      data.push(s.data);
    }
    plotRef.current.setData(data as any);
  }, [t, series]);

  return <div ref={ref} className="w-full" />;
}
