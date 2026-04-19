interface EmptyCanvasProps {
  title: string
  description: string
  onCollect: () => void
  isCollecting: boolean
}

export function EmptyCanvas(props: EmptyCanvasProps) {
  return (
    <div className="flex h-full flex-1 items-center justify-center bg-canvas p-8">
      <div className="canvas-dashed-frame max-w-xl space-y-4 rounded-3xl bg-white p-8 text-center">
        <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-zinc-500">
          Empty state
        </p>
        <h2 className="text-2xl font-semibold text-zinc-950">{props.title}</h2>
        <p className="text-sm leading-6 text-zinc-600">{props.description}</p>
        <button
          type="button"
          onClick={props.onCollect}
          disabled={props.isCollecting}
          className="rounded-2xl border border-zinc-200 px-4 py-3 text-sm font-medium text-zinc-950 transition hover:border-zinc-400 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {props.isCollecting ? 'Collecting…' : 'Collect now'}
        </button>
      </div>
    </div>
  )
}
