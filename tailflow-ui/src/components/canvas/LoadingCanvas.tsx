export function LoadingCanvas() {
  return (
    <div className="relative flex h-full flex-1 overflow-hidden bg-canvas p-8">
      <div className="pointer-events-none absolute inset-0 canvas-loading-grid" />
      <div className="relative h-full w-full">
        <SkeletonNode className="left-[6%] top-[12%]" />
        <SkeletonNode className="left-[38%] top-[42%]" />
        <SkeletonNode className="right-[8%] top-[16%]" />
      </div>
    </div>
  )
}

function SkeletonNode(props: { className: string }) {
  return (
    <div
      className={`absolute h-56 w-80 animate-pulse rounded-2xl border border-zinc-200 bg-white p-5 ${props.className}`}
    >
      <div className="h-4 w-28 rounded bg-zinc-200" />
      <div className="mt-3 h-3 w-24 rounded bg-zinc-100" />
      <div className="mt-7 grid grid-cols-3 gap-3">
        <div className="h-14 rounded-xl bg-zinc-100" />
        <div className="h-14 rounded-xl bg-zinc-100" />
        <div className="h-14 rounded-xl bg-zinc-100" />
      </div>
      <div className="mt-7 space-y-2">
        <div className="h-9 rounded-xl bg-zinc-100" />
        <div className="h-9 rounded-xl bg-zinc-100" />
        <div className="h-9 rounded-xl bg-zinc-100" />
      </div>
    </div>
  )
}
