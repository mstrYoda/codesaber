import { useCallback, useEffect, useRef, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { ModelState } from '../../bindings/codesaber/backend/acp/models'

export default function ModelPicker({ projectId, thinking, onBusy, provider }: {
  projectId: string; thinking: boolean; onBusy: (busy: boolean) => void; provider?: string | null
}) {
  const [models, setModels] = useState<ModelState | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const generation = useRef(0)
  useEffect(() => () => { generation.current++; onBusy(false) }, [onBusy])
  const run = useCallback(async (action: () => Promise<ModelState>) => {
    const request = ++generation.current
    setBusy(true); onBusy(true); setError('')
    try {
      const state = await action()
      if (generation.current === request) setModels(state)
    } catch (e) {
      if (generation.current === request) setError(String(e))
    } finally {
      if (generation.current === request) { setBusy(false); onBusy(false) }
    }
  }, [onBusy])
  const refresh = useCallback(() => run(() => App.ACPModels(projectId)), [projectId, run])
  useEffect(() => { if (!thinking) void refresh() }, [thinking, refresh])
  const options = models?.options ?? []
  return <div className="shrink-0 mb-2 rounded-lg border border-[#333639] p-2 text-[11px]">
    <div className="flex items-center gap-2">
      <label htmlFor="agent-model" className="text-dim">Model</label>
      <select id="agent-model" aria-label="AI model" value={models?.currentId ?? ''}
        disabled={thinking || busy || !options.length}
        className="no-drag min-w-0 flex-1 rounded bg-[#242629] border border-[#414448] p-1.5 text-primary disabled:opacity-50"
        onChange={e => {
          const id = e.target.value
          if (models) void run(() => App.ACPSetModel(projectId, models.sessionId, id))
        }}>
        {!models?.currentId && <option value="">{busy ? 'Loading models…' : 'Provider default'}</option>}
        {models?.currentId && !options.some(o => o.id === models.currentId) && <option value={models.currentId}>{models.currentId}</option>}
        {options.map(option => <option key={option.id} value={option.id} title={option.description}>
          {option.group ? `${option.group} / ` : ''}{option.name}
        </option>)}
      </select>
      <button type="button" aria-label="Refresh models" disabled={busy || thinking}
        className="no-drag text-dim hover:text-primary disabled:opacity-40" onClick={() => void refresh()}>↻</button>
    </div>
    {provider === 'opencode' && models?.currentId && <p className="mt-1 text-dim break-words">
      {models.currentId.startsWith('opencode-go/') ? 'OpenCode Go · subscription' : models.currentId.startsWith('opencode/') ? 'OpenCode Zen · usage-based billing (some models may be free)' : 'Configured provider'}
      <span className="block">{models.currentId}</span>
    </p>}
    {!busy && models && !options.length && <p className="mt-1 text-dim">This provider does not offer model selection.</p>}
    {busy && <p role="status" className="mt-1 text-dim">Updating model settings…</p>}
    {error && <p role="alert" className="mt-1 text-[#e5735f] break-words">{error}</p>}
  </div>
}
