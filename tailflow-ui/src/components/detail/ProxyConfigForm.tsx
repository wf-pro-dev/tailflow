import { useEffect, useMemo, useState } from 'react'
import type { ParseResult, ProxyConfigInput } from '../../api/types'
import { useDeleteProxyConfig } from '../../hooks/useDeleteProxyConfig'
import {
  useProxyConfig,
  useProxyConfigs,
} from '../../hooks/useProxyConfigs'
import { useSetProxyConfig } from '../../hooks/useSetProxyConfig'
import { formatRelativeTime } from '../../lib/time'
import { useRenderLoopGuard } from '../../lib/debug'
import { Badge } from '../shared/Badge'
import { Spinner } from '../shared/Spinner'

interface ProxyConfigFormProps {
  nodeName: string
}

type ProxyKind = 'nginx' | 'caddy'

export function ProxyConfigForm(props: ProxyConfigFormProps) {
  useRenderLoopGuard('ProxyConfigForm')

  const proxyConfigsQuery = useProxyConfigs(props.nodeName)
  const setProxyConfigMutation = useSetProxyConfig()
  const [kind, setKind] = useState<ProxyKind>('nginx')
  const [configPath, setConfigPath] = useState('')
  const [preview, setPreview] = useState<ParseResult | null>(null)
  const [localError, setLocalError] = useState<string | null>(null)

  const latestConfig = useMemo(() => {
    const configs = proxyConfigsQuery.data ?? []
    return [...configs].sort((left, right) =>
      right.updated_at.localeCompare(left.updated_at),
    )[0] ?? null
  }, [proxyConfigsQuery.data])

  useEffect(() => {
    if (!latestConfig) {
      setKind('nginx')
      setConfigPath('')
      return
    }

    setKind((latestConfig.kind === 'caddy' ? 'caddy' : 'nginx') as ProxyKind)
    setConfigPath(latestConfig.config_path)
  }, [latestConfig])

  useEffect(() => {
    setPreview(null)
    setLocalError(null)
  }, [props.nodeName])

  const saveError = localError ?? setProxyConfigMutation.error?.message ?? null

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const trimmedPath = configPath.trim()

    if (!trimmedPath) {
      setLocalError('Config path is required.')
      return
    }

    setLocalError(null)

    try {
      const response = await setProxyConfigMutation.mutateAsync({
        nodeName: props.nodeName,
        request: {
          kind,
          config_path: trimmedPath,
        },
      })

      setPreview(response.preview)
    } catch {
      setPreview(null)
    }
  }

  return (
    <section className="space-y-4">
      <div>
        <p className="text-[12px] font-medium uppercase tracking-[0.18em] text-zinc-500">
          Proxy config
        </p>
        <p className="mt-1 text-sm text-zinc-600">
          Save a node proxy config and preview parsed forwarding actions before
          the topology refresh completes.
        </p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4 rounded-2xl border border-zinc-200 bg-white p-4">
        <div className="space-y-2">
          <label
            htmlFor="proxy-kind"
            className="text-[11px] font-medium uppercase tracking-[0.18em] text-zinc-500"
          >
            Kind
          </label>
          <select
            id="proxy-kind"
            value={kind}
            onChange={(event) => setKind(event.target.value as ProxyKind)}
            className="w-full rounded-xl border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 outline-none transition focus:border-zinc-400"
          >
            <option value="nginx">nginx</option>
            <option value="caddy">caddy</option>
          </select>
        </div>

        <div className="space-y-2">
          <label
            htmlFor="proxy-config-path"
            className="text-[11px] font-medium uppercase tracking-[0.18em] text-zinc-500"
          >
            Config path
          </label>
          <input
            id="proxy-config-path"
            type="text"
            value={configPath}
            onChange={(event) => setConfigPath(event.target.value)}
            placeholder={kind === 'nginx' ? '/etc/nginx/nginx.conf' : '/etc/caddy/Caddyfile'}
            className="w-full rounded-xl border border-zinc-200 bg-white px-3 py-2 text-sm text-zinc-900 outline-none transition focus:border-zinc-400"
          />
        </div>

        {latestConfig ? (
          <div className="rounded-xl border border-zinc-100 bg-canvas px-3 py-3 text-sm text-zinc-600">
            Existing saved config: <span className="font-medium text-zinc-900">{latestConfig.kind}</span>{' '}
            at <span className="font-medium text-zinc-900">{latestConfig.config_path}</span>
          </div>
        ) : null}

        {saveError ? (
          <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-700">
            {saveError}
          </div>
        ) : null}

        <button
          type="submit"
          disabled={setProxyConfigMutation.isPending}
          className="w-full rounded-xl border border-zinc-200 px-3 py-2 text-sm font-medium text-zinc-900 transition hover:border-zinc-400 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {setProxyConfigMutation.isPending ? 'Saving and collecting…' : 'Save and preview'}
        </button>
      </form>

      <ProxyConfigPreview
        preview={preview}
        latestConfig={latestConfig}
        isLoading={proxyConfigsQuery.isPending}
      />

      <SavedProxyConfigs
        nodeName={props.nodeName}
        configs={proxyConfigsQuery.data ?? []}
        isLoading={proxyConfigsQuery.isPending}
        errorMessage={proxyConfigsQuery.error?.message ?? null}
      />
    </section>
  )
}

function ProxyConfigPreview(props: {
  preview: ParseResult | null
  latestConfig: ProxyConfigInput | null
  isLoading: boolean
}) {
  return (
    <div className="space-y-3 rounded-2xl border border-zinc-200 bg-white p-4">
      <div>
        <p className="text-sm font-medium text-zinc-950">Preview</p>
        <p className="mt-1 text-sm text-zinc-600">
          Forward actions returned by the backend parser.
        </p>
      </div>

      {props.isLoading ? (
        <p className="text-sm text-zinc-500">Loading saved configs…</p>
      ) : props.preview?.forwards.length ? (
        <div className="overflow-hidden rounded-xl border border-zinc-200">
          <table className="min-w-full divide-y divide-zinc-200 text-sm">
            <thead className="bg-canvas">
              <tr className="text-left text-[11px] uppercase tracking-[0.18em] text-zinc-500">
                <th className="px-4 py-3 font-medium">Listener</th>
                <th className="px-4 py-3 font-medium">Target</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 bg-white">
              {props.preview.forwards.map((forward, index) => (
                <tr key={`${forward.listener.port}-${forward.target.raw}-${index}`}>
                  <td className="px-4 py-3 text-zinc-900">
                    {forward.listener.addr ? `${forward.listener.addr}:` : ''}
                    {forward.listener.port}
                  </td>
                  <td className="px-4 py-3 text-zinc-600">
                    {forward.target.raw}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : props.preview?.errors?.length ? (
        <div className="space-y-2 rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-700">
          {props.preview.errors.map((error) => (
            <p key={error}>{error}</p>
          ))}
        </div>
      ) : props.latestConfig ? (
        <p className="text-sm text-zinc-500">
          A config is saved for this node. Submit the form to refresh the backend
          preview and trigger a run.
        </p>
      ) : (
        <p className="text-sm text-zinc-500">
          No proxy config has been saved for this node yet.
        </p>
      )}
    </div>
  )
}

function SavedProxyConfigs(props: {
  nodeName: string
  configs: ProxyConfigInput[]
  isLoading: boolean
  errorMessage: string | null
}) {
  const sortedConfigs = useMemo(
    () =>
      [...props.configs].sort((left, right) =>
        right.updated_at.localeCompare(left.updated_at),
      ),
    [props.configs],
  )

  return (
    <div className="space-y-3 rounded-2xl border border-zinc-200 bg-white p-4">
      <div>
        <p className="text-sm font-medium text-zinc-950">Saved configs</p>
        <p className="mt-1 text-sm text-zinc-600">
          Inspect stored rules for this node or remove a config entirely.
        </p>
      </div>

      {props.isLoading ? (
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <Spinner />
          <span>Loading saved configs…</span>
        </div>
      ) : props.errorMessage ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-700">
          {props.errorMessage}
        </div>
      ) : sortedConfigs.length === 0 ? (
        <p className="text-sm text-zinc-500">
          No saved proxy configs for this node.
        </p>
      ) : (
        <div className="space-y-3">
          {sortedConfigs.map((config) => (
            <SavedProxyConfigCard
              key={config.id}
              nodeName={props.nodeName}
              config={config}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function SavedProxyConfigCard(props: {
  nodeName: string
  config: ProxyConfigInput
}) {
  const [isExpanded, setIsExpanded] = useState(false)
  const parsedConfigQuery = useProxyConfig(isExpanded ? props.config.id : null)
  const deleteProxyConfigMutation = useDeleteProxyConfig()

  const handleDelete = async () => {
    const confirmed = window.confirm(
      `Delete saved proxy config ${props.config.config_path}?`,
    )
    if (!confirmed) {
      return
    }

    await deleteProxyConfigMutation.mutateAsync({
      configID: props.config.id,
      nodeName: props.nodeName,
    })
    setIsExpanded(false)
  }

  return (
    <div className="flex flex-col rounded-xl border border-zinc-200 bg-canvas p-4 gap-3">
     
      <div className="flex items-start justify-between">

        <div className="min-w-0">
          <div>
            <Badge>{props.config.kind}</Badge>
          </div>
        </div>


        <div className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            onClick={() => setIsExpanded((current) => !current)}
            className="rounded-full border border-zinc-200 px-3 py-1.5 text-xs font-medium text-zinc-600 transition hover:border-zinc-400 hover:text-zinc-950"
          >
            {isExpanded ? 'Hide rules' : 'See rules'}
          </button>
          <button
            type="button"
            onClick={() => void handleDelete()}
            disabled={deleteProxyConfigMutation.isPending}
            className="rounded-full border border-red-200 px-3 py-1.5 text-xs font-medium text-red-700 transition hover:border-red-300 hover:text-red-800 disabled:cursor-not-allowed disabled:opacity-60"
          >
            {deleteProxyConfigMutation.isPending ? 'Deleting…' : 'Delete'}
          </button>
        </div>

      </div>

      {deleteProxyConfigMutation.error ? (
        <div className="mt-3 rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-700">
          {deleteProxyConfigMutation.error.message}
        </div>
      ) : null}

      <div className="flex flex-1 rounded-xl border border-zinc-200 bg-white p-3 text-sm text-zinc-600">
        <span className="font-medium text-zinc-900">{props.config.config_path}</span>
      </div>

      {isExpanded ? (
        <div >
          {parsedConfigQuery.isPending ? (
            <div className="flex items-center gap-2 text-sm text-zinc-500">
              <Spinner />
              <span>Loading parsed rules…</span>
            </div>
          ) : parsedConfigQuery.error ? (
            <div className="rounded-xl border border-red-200 bg-red-50 px-3 py-3 text-sm text-red-700">
              {parsedConfigQuery.error.message}
            </div>
          ) : parsedConfigQuery.data ? (
            <ParsedProxyConfigRules
              preview={parsedConfigQuery.data.parsed}
              config={parsedConfigQuery.data.config}
            />
          ) : null}

        </div>

      ) : null}

      <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-zinc-500">
        <span>{formatRelativeTime(props.config.updated_at)}</span>
      </div>

    </div>
  )
}

function ParsedProxyConfigRules(props: {
  preview: ParseResult
  config: ProxyConfigInput
}) {
  return (
    <div className="space-y-3">

      {props.preview.forwards.length > 0 ? (
        <div className="overflow-hidden rounded-xl border border-zinc-200 bg-white">
          <table className="min-w-full divide-y divide-zinc-200 text-sm">
            <thead className="bg-canvas">
              <tr className="text-left text-[11px] uppercase tracking-[0.18em] text-zinc-500">
                <th className="px-4 py-3 font-medium">Listener</th>
                <th className="px-4 py-3 font-medium">Target</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-100 bg-white">
              {props.preview.forwards.map((forward, index) => (
                <tr key={`${forward.listener.port}-${forward.target.raw}-${index}`}>
                  <td className="px-4 py-3 text-zinc-900">
                    {forward.listener.addr ? `${forward.listener.addr}:` : ''}
                    {forward.listener.port}
                  </td>
                  <td className="px-4 py-3 text-zinc-600">
                    {forward.target.raw}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {props.preview.errors?.length ? (
        <div className="space-y-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-700">
          {props.preview.errors.map((error) => (
            <p key={error}>{error}</p>
          ))}
        </div>
      ) : null}

      {props.preview.forwards.length === 0 && !props.preview.errors?.length ? (
        <p className="text-sm text-zinc-500">
          No rules were parsed from this saved config.
        </p>
      ) : null}
    </div>
  )
}
