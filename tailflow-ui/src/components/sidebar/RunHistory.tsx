import type { CollectionRun } from '../../api/types'
import { formatRelativeTime, formatTimestamp } from '../../lib/time'
import { Spinner } from '../shared/Spinner'
import { StatusDot } from '../shared/StatusDot'

interface RunHistoryProps {
  runs: CollectionRun[]
  isLoading: boolean
}

export function RunHistory(props: RunHistoryProps) {
  return (
    <section className="flex max-h-72 flex-col">
      <header className="flex items-center justify-between px-6 py-4">
        <div>
          <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-zinc-400">
            Runs
          </p>
          <p className="mt-1 text-sm text-zinc-300">
            Recent collection history
          </p>
        </div>
        {props.isLoading ? <Spinner /> : null}
      </header>

      <div className="overflow-auto px-4 pb-4">
        {props.runs.length === 0 && !props.isLoading ? (
          <div className="rounded-2xl border border-dashed border-zinc-700 px-4 py-5 text-sm leading-6 text-zinc-400">
            No collection run has completed yet.
          </div>
        ) : (
          <div className="space-y-2">
            {props.runs.slice(0, 8).map((run) => (
              <article
                key={run.id}
                className="rounded-2xl border border-zinc-800 bg-zinc-950/40 px-4 py-3"
              >
                <div className="flex items-center justify-between gap-3">
                  <StatusDot
                    tone={run.error_count > 0 ? 'warning' : 'online'}
                    label={run.error_count > 0 ? 'Warnings' : 'Successful'}
                    surface="dark"
                    emphasize
                  />
                  <p className="font-mono text-[11px] text-zinc-500">
                    {run.id.slice(0, 8)}
                  </p>
                </div>
                <p
                  className="mt-3 text-sm font-medium text-white"
                  title={formatTimestamp(run.finished_at)}
                >
                  {formatRelativeTime(run.finished_at)}
                </p>
                <div className="mt-3 flex items-center gap-4 text-xs text-zinc-400">
                  <span>{run.node_count} nodes</span>
                  <span>{run.error_count} errors</span>
                </div>
                <p
                  className="mt-2 text-[11px] text-zinc-500"
                  title={formatTimestamp(run.started_at)}
                >
                  Started {formatRelativeTime(run.started_at)}
                </p>
              </article>
            ))}
          </div>
        )}
      </div>
    </section>
  )
}
