interface ErrorStateProps {
  title: string
  description: string
  details?: string
}

export function ErrorState(props: ErrorStateProps) {
  return (
    <div className="flex h-full min-h-96 items-center justify-center bg-canvas p-8">
      <div className="max-w-xl space-y-3 rounded-2xl border border-red-200 bg-white p-6">
        <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-red-500">
          Error
        </p>
        <h2 className="text-xl font-semibold text-zinc-950">{props.title}</h2>
        <p className="text-sm leading-6 text-zinc-700">{props.description}</p>
        {props.details ? (
          <pre className="overflow-auto rounded-xl border border-zinc-200 bg-canvas p-4 text-xs leading-5 text-zinc-700">
            {props.details}
          </pre>
        ) : null}
      </div>
    </div>
  )
}
