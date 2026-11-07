// StreamBanner — shows a non-blocking banner when the SSE stream is lost
// or in fallback polling mode. Design §17.6 "Stream lost" state, §17.4 last
// bullet ("If the stream cannot be opened or fails three times in a row,
// queries fall back to refetchInterval: 10_000 and a small banner says
// 'Live updates paused'").

interface Props {
  status: "connecting" | "open" | "reconnecting" | "closed" | "fallback";
}

export function StreamBanner({ status }: Props) {
  if (status === "open" || status === "connecting") return null;

  const messages: Record<string, string> = {
    reconnecting: "Reconnecting…",
    closed: "Live updates paused — stream closed",
    fallback: "Live updates paused — polling every 10s",
  };

  return (
    <div className="mb-4 rounded-md border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-800 dark:border-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
      {messages[status] ?? "Stream disconnected"}
    </div>
  );
}
