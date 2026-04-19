import { Spinner } from '../shared/Spinner'

interface TriggerButtonProps {
  onClick: () => void
  isLoading: boolean
}

export function TriggerButton(props: TriggerButtonProps) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      disabled={props.isLoading}
      className="flex w-full items-center justify-between gap-4 rounded-2xl border border-zinc-700 bg-white px-4 py-3 text-left text-zinc-950 transition hover:border-zinc-500 disabled:cursor-not-allowed disabled:opacity-60"
    >
      <div>
        <p className="text-sm font-medium">
          {props.isLoading ? 'Collecting…' : 'Collect now'}
        </p>
        <p className="mt-1 text-xs text-zinc-500">
          Start a new collection cycle against the active tailnet.
        </p>
      </div>
      {props.isLoading ? <Spinner /> : <span className="text-lg leading-none">+</span>}
    </button>
  )
}
