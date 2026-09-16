import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useProjects } from '../state/projects'
import { APP_VERSION } from '../version'
import { useSettings } from '../lib/settings'
import { shortcut } from '../lib/platform'

interface Command {
  id: string
  name: string
  hint: string
  danger?: boolean
  run: () => void
}

// subsequence returns true when query chars appear in name in order
// (case-insensitive, no gaps scoring — MVP fuzzy).
const subsequence = (query: string, name: string): boolean => {
  const q = query.toLowerCase()
  const n = name.toLowerCase()
  let i = 0
  for (let j = 0; j < n.length && i < q.length; j++) {
    if (q[i] === n[j]) i++
  }
  return i === q.length
}

const CommandPalette: React.FC = () => {
  const { projects, activeId, open, remove } = useProjects()
  const { settings, update } = useSettings()
  const [openState, setOpenState] = useState(false)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const [confirmingId, setConfirmingId] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  const close = useCallback(() => {
    setOpenState(false)
    setQuery('')
    setSelected(0)
    setConfirmingId(null)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'p') {
        e.preventDefault()
        setOpenState((prev) => {
          if (prev) {
            setQuery('')
            setSelected(0)
            setConfirmingId(null)
            return false
          }
          return true
        })
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (openState) inputRef.current?.focus()
  }, [openState])

  const active = projects.find((p) => p.id === activeId) ?? null

  const commands: Command[] = useMemo(() => {
    const cmds: Command[] = [
      {
        id: 'open-folder',
        name: 'Open Folder…',
        hint: 'Pick a folder and open it as a project',
        run: () => void open(),
      },
    ]
    if (active) {
      cmds.push({
        id: 'remove-project',
        name: confirmingId === active.id ? `Confirm: Remove "${active.name}"?` : 'Remove Project…',
        hint: confirmingId === active.id ? 'Press Enter again to confirm' : active.root,
        danger: true,
        run: () => {
          if (confirmingId === active.id) {
            void remove(active.id)
            close()
          } else {
            setConfirmingId(active.id)
          }
        },
      })
    }
    cmds.push({
      id: 'toggle-bracket-colors',
      name: `${settings.bracketColors ? '\u2611' : '\u2610'} Toggle Bracket Colorization`,
      hint: `Bracket pair colors: ${settings.bracketColors ? 'on' : 'off'} (persisted)`,
      run: () => update({ bracketColors: !settings.bracketColors }),
    })
    cmds.push({
      id: 'toggle-perf-hud',
      name: `${settings.perfHud ? '☑' : '☐'} Toggle Perf HUD`,
      hint: `Live IDE health overlay (${shortcut('Shift+H')}, persisted)`,
      run: () => update({ perfHud: !settings.perfHud }),
    })
    cmds.push({
      id: 'about',
      name: 'About codesaber',
      hint: `codesaber ${APP_VERSION}`,
      run: () => close(),
    })
    return cmds
  }, [active, confirmingId, open, remove, close, settings, update])

  const filtered = useMemo(
    () => commands.filter((c) => subsequence(query, c.name)),
    [commands, query],
  )

  const selectIdx = Math.min(selected, Math.max(0, filtered.length - 1))
  const current = filtered[selectIdx]

  const runSelected = useCallback(() => {
    current?.run()
    if (current && current.id !== 'remove-project') close()
  }, [current, close])

  const onInputKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelected((i) => Math.min(i + 1, filtered.length - 1))
      if (confirmingId) setConfirmingId(null)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelected((i) => Math.max(i - 1, 0))
      if (confirmingId) setConfirmingId(null)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      runSelected()
    }
  }

  if (!openState) return null

  return (
    <div
      data-command-palette="open"
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close()
      }}
    >
      <div
        className="mt-[12vh] w-[480px] max-w-[80vw] rounded-lg bg-panel border border-panel shadow-2xl overflow-hidden"
        onKeyDown={onInputKey}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setSelected(0)
            setConfirmingId(null)
          }}
          placeholder="Type a command…"
          className="w-full px-3 py-2.5 bg-transparent outline-none text-primary text-[13px] border-b border-panel placeholder:text-dim"
        />
        <div className="max-h-[40vh] overflow-y-auto py-1">
          {filtered.map((c, i) => (
            <div
              key={c.id}
              className={
                'flex items-center gap-2 px-3 py-1.5 text-[12px] cursor-default ' +
                (i === selectIdx ? 'bg-[#373940]' : 'hover:bg-[#2e3037]')
              }
              onMouseEnter={() => setSelected(i)}
              onClick={runSelected}
            >
              <span className={c.danger ? 'text-[#e53c34]' : 'text-primary'}>
                {c.name}
              </span>
            </div>
          ))}
          {filtered.length === 0 && (
            <div className="px-3 py-2 text-dim text-[11px]">No matching commands</div>
          )}
        </div>
        <div className="px-3 py-1.5 border-t border-panel text-dim text-[10px] truncate">
          {current?.hint ?? '\u21b5 to run \u00b7 esc to close'}
        </div>
      </div>
    </div>
  )
}

export default CommandPalette
