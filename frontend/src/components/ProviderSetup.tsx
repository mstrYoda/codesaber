import { useEffect, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { Info, Diagnostic } from '../../bindings/codesaber/backend/acp/models'

export default function ProviderSetup({ projectId, onStart }: { projectId: string; onStart: (name: string) => Promise<void> }) {
  const [providers, setProviders] = useState<Info[]>([])
  const [selected, setSelected] = useState('claude')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [diagnostic, setDiagnostic] = useState<Diagnostic | null>(null)
  const current = providers.find(p => p.name === selected)
  const refresh = async () => setProviders((await App.ACPHarnesses()) ?? [])
  useEffect(() => { void refresh().catch(() => setError('Could not check installed providers. Try again.')) }, [])
  useEffect(() => { setDiagnostic(null); setError(''); setNotice('') }, [selected, projectId])
  const run = async (label: string, action: () => Promise<void>) => {
    setBusy(label); setError(''); setNotice(''); setDiagnostic(null)
    try { await action() } catch (e) { setError(String(e)) } finally { setBusy('') }
  }
  const button = 'no-drag rounded border border-[#414448] px-2 py-1.5 text-primary hover:bg-white/5 disabled:opacity-40 disabled:cursor-not-allowed'
  return <section aria-label="AI provider setup" className="shrink-0 mb-2 rounded-lg border border-[#333639] p-2 text-[11px] max-h-[45%] overflow-y-auto">
    <label className="flex items-center justify-between gap-2">
      <span>AI provider</span>
      <select aria-label="AI provider" value={selected} disabled={!!busy}
        className="no-drag rounded bg-[#242629] border border-[#414448] p-1.5 text-primary"
        onChange={e => setSelected(e.target.value)}>
        {['claude', 'antigravity', 'opencode'].map(name => <option key={name} value={name}>{name === 'claude' ? 'Claude' : name === 'antigravity' ? 'Antigravity (Google)' : 'OpenCode'}</option>)}
      </select>
    </label>
    <p className="my-2 text-dim">{current?.managed ? `Managed version ${current.version}` : current?.available ? 'Existing installation found' : 'Setup required. CodeSaber installs the provider in its own folder.'}</p>
    {selected === 'antigravity' && <p className="my-2 text-dim">Official Google ACP server; a separate Google sign-in is required. Download: about 468 MB. Windows x64 managed install.</p>}
    {selected === 'opencode' && <p className="my-2 text-dim">Supports OpenCode Go (subscription), OpenCode Zen (usage-based billing), and other configured providers. Use Sign in and choose the connection you want. After connecting, start a new session and select its model. A listed model does not confirm account access or quota.</p>}
    <div className="flex flex-wrap gap-1.5">
      <button className={button} disabled={!!busy} onClick={() => void run('Downloading and installing… This can take several minutes.', async () => {
        await App.ACPInstall(selected, current?.managed ?? false); await refresh(); setNotice('Installed. Check the connection or sign in to continue.')
      })}>{current?.managed ? 'Repair installation' : current?.available ? 'Install managed version' : 'Install provider'}</button>
      <button className={button} disabled={!!busy || !current?.available} onClick={() => void run('Checking connection…', async () => {
        setDiagnostic(await App.ACPCheck(projectId, selected))
      })}>Check connection</button>
      <button className={button} disabled={!!busy || !current?.available} onClick={() => void run('Complete sign-in in the provider browser or terminal window…', async () => {
        await App.ACPLogin(selected); setNotice(selected === 'antigravity' ? 'Signed in. Check the connection to continue.' : 'Complete sign-in in the provider window, then check the connection here.')
      })}>Sign in</button>
      <button className={button + ' bg-[var(--accent)] !text-[#0b0c10] font-medium'} disabled={!!busy || !current?.available}
        onClick={() => void run('Starting session…', () => onStart(selected))}>Start</button>
      <button className={button} disabled={!!busy} onClick={() => void run('Refreshing…', refresh)}>Refresh</button>
    </div>
    <label className="mt-2 flex items-start gap-2 text-dim">
      <input type="checkbox" checked={current?.isolated ?? false} disabled={!!busy || !current}
        onChange={e => { const checked = e.target.checked; void run('Saving settings…', async () => { await App.ACPUseSeparateSettings(selected, checked); await refresh() }) }} />
      <span>Use separate provider settings. Keeps your original settings unchanged; you may need to sign in again. API environment variables are still inherited.</span>
    </label>
    {busy && <p role="status" className="mt-2 text-[#e6c07b]">{busy}</p>}
    {busy.startsWith('Downloading') && <button className={button + ' mt-1'} onClick={() => void App.ACPCancelInstall().catch(e => setError(String(e)))}>Cancel setup</button>}
    {notice && <p role="status" className="mt-2 text-dim">{notice}</p>}
    {error && <p role="alert" className="mt-2 text-[#e5735f] break-words">{error}</p>}
    {diagnostic && <div className="mt-2 break-words" role="status">
      <p className={diagnostic.ready ? 'text-[#7dcf9e]' : 'text-[#e6c07b]'}>{diagnostic.message}</p>
      {diagnostic.model && <p className="text-dim">Provider model: {diagnostic.model}</p>}
      {diagnostic.warnings?.map(w => <p key={w} className="mt-1 text-[#e6c07b]">{w}</p>)}
    </div>}
  </section>
}
