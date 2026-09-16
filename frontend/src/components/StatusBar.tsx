import React, { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as App from '../../bindings/codesaber/backend/app'
import { useLayout } from '../state/layout'
import { useProjects } from '../state/projects'
import { useGit } from '../state/git'
import { useTabs } from '../state/tabs'
import { useDiagCounts } from '../state/diagstore'
import { extOf } from '../lib/filetype'
import { APP_VERSION } from '../version'

// EngineStatus mirrors the payload of the backend "engine.status" event.
interface EngineStatus {
  engine: string
  ok: boolean
  projectId?: string
}

// EnginePills tracks engine health. Baseline: a green "backend" dot after the
// first successful ListProjects round-trip, then updated by "engine.status"
// events (green ok:true, red ok:false). Unknown engines are added on arrival.
const EnginePills: React.FC = () => {
  const [statuses, setStatuses] = useState<EngineStatus[]>([])
  const [backendOk, setBackendOk] = useState(false)

  useEffect(() => {
    let disposed = false
    App.ListProjects()
      .then(() => {
        if (!disposed) setBackendOk(true)
      })
      .catch(() => {
        if (!disposed) setBackendOk(false)
      })
    return () => {
      disposed = true
    }
  }, [])

  useEffect(() => {
    const off = Events.On('engine.status', (ev: any) => {
      const st = ev.data as EngineStatus
      if (!st?.engine) return
      setStatuses((prev) => {
        const rest = prev.filter((s) => s.engine !== st.engine)
        return [...rest, st]
      })
    })
    return () => {
      off()
    }
  }, [])

  const pillEngines = ['backend', ...statuses.map((s) => s.engine)]

  return (
    <div className="flex items-center gap-1.5">
      {pillEngines.map((engine) => {
        const st = statuses.find((s) => s.engine === engine)
        const ok = engine === 'backend' ? backendOk : (st?.ok ?? false)
        const label =
          engine === 'backend'
            ? backendOk
              ? 'backend ok'
              : 'backend\u2026'
            : `${engine}${ok ? '' : ' down'}`
        return (
          <span
            key={engine}
            className="flex items-center gap-1"
            title={engine === 'backend' ? 'Wails RPC' : `engine.status: ${st?.ok ? 'ok' : 'failed'}`}
          >
            <span
              className={
                'w-1.5 h-1.5 rounded-full ' +
                (ok
                  ? 'bg-[#4a9e6b]'
                  : engine === 'backend' && !st
                    ? 'bg-[#73767b]'
                    : 'bg-[#c75454]')
              }
            />
            <span className="text-dim">{label}</span>
          </span>
        )
      })}
    </div>
  )
}

// LSPStatus mirrors the payload of the backend "lsp.state" event.
interface LSPStatus {
  running: boolean
  reason?: string
}

// GoplsPill shows a green "gopls" dot when the project's language server is
// running and a gray one on failure (with the reason as tooltip). Only
// appears once some state has been reported for the active project.
const GoplsPill: React.FC<{ projectId: string | null }> = ({ projectId }) => {
  const [byProject, setByProject] = useState<Record<string, LSPStatus>>({})

  useEffect(() => {
    const off = Events.On('lsp.state', (ev: any) => {
      const st = ev.data as { projectId?: string; running?: boolean; reason?: string }
      if (!st?.projectId) return
      setByProject((prev) => ({
        ...prev,
        [st.projectId!]: { running: !!st.running, reason: st.reason },
      }))
    })
    return () => off()
  }, [])

  const st = projectId ? byProject[projectId] : undefined
  if (!st) return null
  const ok = st.running
  return (
    <span
      className="flex items-center gap-1"
      title={ok ? 'gopls running' : `gopls: ${st.reason ?? 'not running'}`}
    >
      <span
        className={
          'w-1.5 h-1.5 rounded-full ' + (ok ? 'bg-[#4a9e6b]' : 'bg-[#73767b]')
        }
      />
      <span className="text-dim">{ok ? 'gopls' : 'gopls: not found'}</span>
    </span>
  )
}

// extToLabel maps a file extension to the human language name shown in the
// status bar (active-tab aware; unknown extensions show the raw ext).
const extToLabel: Record<string, string> = {
  ts: 'TypeScript',
  tsx: 'TypeScript JSX',
  mts: 'TypeScript',
  cts: 'TypeScript',
  js: 'JavaScript',
  jsx: 'JavaScript JSX',
  mjs: 'JavaScript',
  cjs: 'JavaScript',
  go: 'Go',
  php: 'PHP',
  rs: 'Rust',
  py: 'Python',
  html: 'HTML',
  htm: 'HTML',
  css: 'CSS',
  scss: 'SCSS',
  json: 'JSON',
  md: 'Markdown',
  sh: 'Shell Script',
  bash: 'Shell Script',
  zsh: 'Shell Script',
  yml: 'YAML',
  yaml: 'YAML',
  toml: 'TOML',
  sql: 'SQL',
  wasm: 'WebAssembly',
  dockerfile: 'Dockerfile',
  makefile: 'Makefile',
}

// languageLabel derives the status-bar language name from the active path.
// Extension-less filenames (Dockerfile, Makefile) match on the basename.
const languageLabel = (path: string | null | undefined): string => {
  if (!path) return ''
  const base = path.slice(path.lastIndexOf('/') + 1).toLowerCase()
  const ext = extOf(path)
  if (!ext && base) return extToLabel[base] ?? base
  return extToLabel[ext] ?? (ext ? ext.toUpperCase() : '')
}

// CursorPos tracks the caret of the active editor via "codesaber:cursor" window
// events (published by the CM6 update listener, debounced 100ms). Switching
// tabs resets to the dim placeholder until the new editor reports.
interface CursorPos {
  line: number
  col: number
}

const CursorPosition: React.FC<{ resetKey: string | null }> = ({ resetKey }) => {
  const [pos, setPos] = useState<CursorPos | null>(null)
  const lastKey = useRef<string | null>(resetKey)

  useEffect(() => {
    if (lastKey.current !== resetKey) {
      lastKey.current = resetKey
      setPos(null)
    }
  }, [resetKey])

  useEffect(() => {
    const onCursor = (e: Event) => {
      const d = (e as CustomEvent<CursorPos>).detail
      if (!d || typeof d.line !== 'number' || typeof d.col !== 'number') return
      setPos(d)
    }
    window.addEventListener('codesaber:cursor', onCursor)
    return () => window.removeEventListener('codesaber:cursor', onCursor)
  }, [])

  return (
    <span className={pos ? 'text-dim' : 'text-dim opacity-60'}>
      {pos ? `Ln ${pos.line}, Col ${pos.col}` : 'Ln 1, Col 1'}
    </span>
  )
}

const Divider: React.FC = () => (
  <span className="w-px h-3.5 bg-[#3a3c3f]" aria-hidden="true" />
)

// StatusHint flashes a brief transient message via "codesaber:status-hint" window
// events (e.g. ⌘K pressed with no selection). Auto-clears after 2.5s.
const StatusHint: React.FC = () => {
  const [text, setText] = useState('')
  const timerRef = useRef<number | undefined>(undefined)
  useEffect(() => {
    const onHint = (e: Event) => {
      const detail = (e as CustomEvent<string>).detail
      setText(detail ? String(detail) : 'select code to edit')
      window.clearTimeout(timerRef.current)
      timerRef.current = window.setTimeout(() => setText(''), 2500)
    }
    window.addEventListener('codesaber:status-hint', onHint)
    return () => {
      window.removeEventListener('codesaber:status-hint', onHint)
      window.clearTimeout(timerRef.current)
    }
  }, [])
  if (!text) return null
  return <span className="text-primary">{text}</span>
}

const StatusBar: React.FC = () => {
  const { ui, toggle } = useLayout()
  const { activeId } = useProjects()
  const { status, sync } = useGit()
  const { tabsByProject } = useTabs()
  const diag = useDiagCounts(activeId)

  const gitStatus = activeId ? status[activeId] : undefined
  const branch = gitStatus?.branch || '(unknown)'
  const dirty =
    !!gitStatus &&
    (gitStatus.staged?.length ?? 0) +
      (gitStatus.unstaged?.length ?? 0) +
      (gitStatus.untracked?.length ?? 0) >
      0
  const syncInfo = activeId ? sync[activeId] : undefined
  const hasRemote = !!syncInfo && syncInfo.remote !== ''

  const tabs = activeId ? tabsByProject[activeId] : undefined
  const activePath =
    tabs && tabs.active && !tabs.active.startsWith('\u0394:')
      ? tabs.active
      : null
  const lang = languageLabel(activePath)

  return (
    <div className="h-6 shrink-0 flex items-center justify-between px-3 bg-[#313336] border-t border-panel text-[11px] text-dim select-none">
      {/* LEFT: branch chip, sync, diag counters, engine pills */}
      <div className="flex items-center gap-2.5 min-w-0">
        <span
          className="px-2 rounded bg-[#1e1f22] text-primary h-4 flex items-center"
          title={dirty ? 'Working tree has changes' : 'Working tree clean'}
        >
          {'\u2387'} {branch}
          {dirty ? '*' : ''}
        </span>
        {hasRemote && (
          <span
            className="flex items-center gap-1.5"
            title={`${syncInfo!.remote}: ${syncInfo!.ahead} ahead, ${syncInfo!.behind} behind`}
          >
            <span className="text-dim">{'\u2193'}{syncInfo!.behind}</span>
            <span className="text-dim">{'\u2191'}{syncInfo!.ahead}</span>
          </span>
        )}
        <span className="flex items-center gap-1" title="Errors">
          <span className="text-[#c75454] leading-none">{'\u25cf'}</span>
          <span className="text-dim">{diag.errors}</span>
        </span>
        <span className="flex items-center gap-1" title="Warnings">
          <span className="text-[#bb9a3c] leading-none">{'\u25b2'}</span>
          <span className="text-dim">{diag.warnings}</span>
        </span>
        <EnginePills />
        <GoplsPill projectId={activeId} />
        <StatusHint />
      </div>
      {/* RIGHT: cursor, indentation, encoding, language, version */}
      <div className="flex items-center gap-2.5">
        <CursorPosition resetKey={activeId ? `${activeId}:${activePath ?? ''}` : null} />
        <span>Spaces: 2</span>
        <span>UTF-8</span>
        {lang && <span>{lang}</span>}
        <Divider />
        <span>codesaber {APP_VERSION}</span>
        <button
          onClick={() => toggle('terminal')}
          className="no-drag w-5 h-4 rounded flex items-center justify-center text-dim hover:text-primary hover:bg-[#373940]"
          title={ui.terminal ? 'Hide terminal (⌘J)' : 'Show terminal (⌘J)'}
          aria-label="Toggle terminal panel"
        >
          {'\u2325'}
        </button>
      </div>
    </div>
  )
}

export default StatusBar
