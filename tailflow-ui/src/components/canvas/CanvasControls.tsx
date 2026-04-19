import { Panel, useReactFlow } from '@xyflow/react'

export function CanvasControls() {
  const reactFlow = useReactFlow()

  return (
    <Panel position="top-right">
      <div className="flex items-center gap-2 rounded-2xl border border-zinc-200 bg-white p-2">
        <CanvasButton label="Zoom in" onClick={() => void reactFlow.zoomIn()} text="+" />
        <CanvasButton label="Zoom out" onClick={() => void reactFlow.zoomOut()} text="−" />
        <CanvasButton
          label="Fit view"
          onClick={() => void reactFlow.fitView({ padding: 0.16, duration: 250 })}
          text="Fit"
        />
      </div>
    </Panel>
  )
}

function CanvasButton(props: {
  label: string
  onClick: () => void
  text: string
}) {
  return (
    <button
      type="button"
      aria-label={props.label}
      onClick={props.onClick}
      className="rounded-xl border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-700 transition hover:border-zinc-400 hover:text-zinc-950"
    >
      {props.text}
    </button>
  )
}
