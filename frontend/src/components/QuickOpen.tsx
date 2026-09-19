import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useProjects } from '../state/projects'
import { useTabs } from '../state/tabs'
import { useSymbols, type SymHit } from '../state/symbols'
import { useKeyboardShortcut } from '../state/keybinding'
import * as App from '../../bindings/codesaber/backend/app'

interface FileItem {
  projectId: string
  projectName: string
  root: string
  path: string
  name: string
  rel: string
}

type Mode = 'files' | 'symbols'

type ResultItem =
  | { type: 'file'; item: FileItem }
  | { type: 'symbol'; hit: SymHit }

const maxResults = 50

const kindLabel: Record<SymHit['kind'], string> = {
  function: 'fn',
  method: 'm',
  class: 'class',
  type: 'type',
  var: 'var',
}

// fuzzyScore returns a match score for query against path (case-insensitive
// subsequence with a contiguous-run bonus), or -1 when not a subsequence.
// Shorter paths win ties.
const fuzzyScore = (query: string, path: string): number => {
  const q = query.toLowerCase()
  const s = path.toLowerCase()
  let qi = 0
  let score = 0
  let prev = -2
  for (let i = 0; i < s.length && qi < q.length; i++) {
    if (s[i] === q[qi]) {
      score += 1 + (i === prev + 1 ? 2 : 0)
      prev = i
      qi++
    }
  }
  if (qi < q.length) return -1
  return score
}

const QuickOpen: React.FC = () => {
  const { projects, setActive } = useProjects()
  const { openFile, tabsByProject } = useTabs()
  const {
    status,
    progress,
    stale,
    count,
    indexVersion,
    ensureScanned,
    query: querySymbols,
  } = useSymbols()
  const [openState, setOpenState] = useState(false)
  const [mode, setMode] = useState<Mode>('files')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const [files, setFiles] = useState<FileItem[]>([])
  const inputRef = useRef<HTMLInputElement>(null)
  const cacheRef = useRef<Map<string, FileItem[]>>(new Map())

  const close = useCallback(() => {
    setOpenState(false)
    setQuery('')
    setSelected(0)
  }, [])

  const open = useCallback((m: Mode) => {
    setOpenState((prev) => {
      if (prev) {
        setQuery('')
        setSelected(0)
        return false
      }
      setMode(m)
      setQuery('')
      setSelected(0)
      return true
    })
  }, [])

  // Mod-P opens the file finder, Mod-T the symbol finder. The command
  // palette takes priority: when it is on screen both are ignored so the
  // overlays never stack.
  const noCommandPalette = useCallback(
    () => !document.querySelector('[data-command-palette]'),
    [],
  )
  useKeyboardShortcut(
    'quickOpenFiles',
    useCallback(() => open('files'), [open]),
    noCommandPalette,
  )
  useKeyboardShortcut(
    'quickOpenSymbols',
    useCallback(() => open('symbols'), [open]),
    noCommandPalette,
  )

  // 'codesaber:quickopen' (activity rail) opens the palette too. Same priority
  // rule as Mod-P: never stack on top of the command palette.
  useEffect(() => {
    const onOpen = () => {
      if (document.querySelector('[data-command-palette]')) return
      open('files')
    }
    window.addEventListener('codesaber:quickopen', onOpen)
    return () => window.removeEventListener('codesaber:quickopen', onOpen)
  }, [open])

  useEffect(() => {
    if (openState) {
      inputRef.current?.focus()
      if (mode === 'symbols') ensureScanned()
    }
  }, [openState, mode, ensureScanned])

  // Lazily index every open project via the backend walker; results are
  // cached per project root until the project set changes.
  useEffect(() => {
    if (!openState || mode !== 'files') return
    const roots = new Set(projects.map((p) => p.root))
    for (const root of cacheRef.current.keys()) {
      if (!roots.has(root)) cacheRef.current.delete(root)
    }
    let disposed = false
    const collected: FileItem[] = []
    const apply = () => {
      if (!disposed) setFiles([...collected])
    }
    for (const p of projects) {
      const cached = cacheRef.current.get(p.root)
      if (cached) {
        collected.push(...cached)
        apply()
        continue
      }
      void App.IndexFiles(p.root)
        .then((paths) => {
          if (disposed) return
          const items: FileItem[] = (paths ?? []).map((path) => ({
            projectId: p.id,
            projectName: p.name,
            root: p.root,
            path,
            name: path.slice(path.lastIndexOf('/') + 1),
            rel: path.startsWith(p.root + '/')
              ? path.slice(p.root.length + 1)
              : path,
          }))
          cacheRef.current.set(p.root, items)
          collected.push(...items)
          apply()
        })
        .catch(() => {})
    }
    return () => {
      disposed = true
    }
  }, [openState, mode, projects])

  // Empty query shows last-opened files: most recently active tab first,
  // projects in recency order.
  const recent = useMemo<FileItem[]>(() => {
    const out: FileItem[] = []
    for (const p of projects) {
      const st = tabsByProject[p.id]
      if (!st) continue
      const ordered = [
        ...st.open.filter((t) => t.path === st.active),
        ...st.open.filter((t) => t.path !== st.active),
      ]
      for (const t of ordered) {
        out.push({
          projectId: p.id,
          projectName: p.name,
          root: p.root,
          path: t.path,
          name: t.title,
          rel: t.path.startsWith(p.root + '/')
            ? t.path.slice(p.root.length + 1)
            : t.path,
        })
      }
    }
    return out
  }, [projects, tabsByProject])

  const fileResults = useMemo<FileItem[]>(() => {
    if (query === '') return recent
    const scored: { item: FileItem; score: number }[] = []
    for (const item of files) {
      const score = fuzzyScore(query, item.rel)
      if (score >= 0) scored.push({ item, score })
    }
    scored.sort(
      (a, b) =>
        b.score - a.score ||
        a.item.rel.length - b.item.rel.length ||
        (a.item.rel < b.item.rel ? -1 : 1),
    )
    return scored.slice(0, maxResults).map((s) => s.item)
  }, [query, files, recent])

  const symbolResults = useMemo<SymHit[]>(() => {
    if (mode !== 'symbols') return []
    void indexVersion // re-run when the index changes
    return querySymbols(query.trim())
  }, [mode, query, indexVersion, querySymbols])

  const results = useMemo<ResultItem[]>(
    () =>
      mode === 'files'
        ? fileResults.map((item) => ({ type: 'file', item }) as ResultItem)
        : symbolResults.map((hit) => ({ type: 'symbol', hit }) as ResultItem),
    [mode, fileResults, symbolResults],
  )

  const selectIdx = Math.min(selected, Math.max(0, results.length - 1))

  const pick = useCallback(
    (r: ResultItem) => {
      if (r.type === 'file') {
        setActive(r.item.projectId)
        void openFile(r.item.projectId, r.item.path)
      } else {
        setActive(r.hit.projectId)
        void openFile(r.hit.projectId, r.hit.path, {
          reveal: { line: r.hit.line - 1, character: r.hit.col - 1 },
        })
      }
      close()
    },
    [setActive, openFile, close],
  )

  const switchMode = useCallback((m: Mode) => {
    setMode(m)
    setQuery('')
    setSelected(0)
  }, [])

  const onInputKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      close()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelected((i) => Math.min(i + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelected((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const item = results[selectIdx]
      if (item) pick(item)
    }
  }

  if (!openState) return null

  const statusLine =
    mode === 'symbols'
      ? progress
        ? `Indexing ${progress.done}/${progress.total}\u2026`
        : stale
          ? `${count} symbols \u00b7 stale \u2014 will rescan`
          : status === 'ready'
            ? `${count} symbols`
            : ''
      : ''

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/40"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close()
      }}
    >
      <div
        className="mt-[12vh] w-[560px] max-w-[80vw] rounded-lg bg-panel border border-panel shadow-2xl overflow-hidden"
        onKeyDown={onInputKey}
      >
        <div className="flex items-center gap-1 px-2 pt-2 border-b border-panel">
          <button
            className={
              'px-2 py-1 rounded text-[10px] cursor-default ' +
              (mode === 'files'
                ? 'bg-[#373940] text-primary'
                : 'text-dim hover:text-primary')
            }
            onClick={() => switchMode('files')}
          >
            Files {'\u2318'}P
          </button>
          <button
            className={
              'px-2 py-1 rounded text-[10px] cursor-default ' +
              (mode === 'symbols'
                ? 'bg-[#373940] text-primary'
                : 'text-dim hover:text-primary')
            }
            onClick={() => switchMode('symbols')}
          >
            Symbols {'\u2318'}T
          </button>
          {statusLine && (
            <span className="ml-auto text-[10px] text-dim pr-1">
              {statusLine}
            </span>
          )}
        </div>
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            const v = e.target.value
            if (mode === 'files' && v.startsWith('sym:')) {
              setMode('symbols')
              setQuery(v.slice(4))
            } else {
              setQuery(v)
            }
            setSelected(0)
          }}
          placeholder={
            mode === 'files'
              ? 'Search files by name\u2026 (sym: for symbols)'
              : 'Search symbols by name\u2026'
          }
          className="w-full px-3 py-2.5 bg-transparent outline-none text-primary text-[13px] border-b border-panel placeholder:text-dim"
        />
        <div className="max-h-[40vh] overflow-y-auto py-1">
          {results.map((r, i) =>
            r.type === 'file' ? (
              <div
                key={r.item.projectId + '\0' + r.item.path}
                className={
                  'px-3 py-1.5 cursor-default ' +
                  (i === selectIdx ? 'bg-[#373940]' : 'hover:bg-[#2e3037]')
                }
                onMouseEnter={() => setSelected(i)}
                onClick={() => pick(r)}
              >
                <div className="text-[12px] text-primary truncate">
                  {r.item.name}
                </div>
                <div className="text-[10px] text-dim truncate">
                  {r.item.projectName}/{r.item.rel}
                </div>
              </div>
            ) : (
              <div
                key={r.hit.projectId + '\0' + r.hit.path + '\0' + r.hit.name + '\0' + r.hit.line}
                className={
                  'px-3 py-1.5 cursor-default flex items-baseline gap-2 ' +
                  (i === selectIdx ? 'bg-[#373940]' : 'hover:bg-[#2e3037]')
                }
                onMouseEnter={() => setSelected(i)}
                onClick={() => pick(r)}
              >
                <span className="text-[9px] uppercase text-dim border border-panel rounded px-1 shrink-0">
                  {kindLabel[r.hit.kind]}
                </span>
                <span className="text-[12px] text-primary truncate">
                  {r.hit.name}
                </span>
                <span className="ml-auto text-[10px] text-dim truncate shrink-0">
                  {r.hit.rel}:{r.hit.line}
                </span>
              </div>
            ),
          )}
          {results.length === 0 && (
            <div className="px-3 py-2 text-dim text-[11px]">
              {mode === 'files'
                ? 'No matching files'
                : status === 'scanning'
                  ? 'Indexing symbols\u2026'
                  : 'No matching symbols'}
            </div>
          )}
        </div>
        <div className="px-3 py-1.5 border-t border-panel text-dim text-[10px]">
          {'\u21b5 to open \u00b7 esc to close'}
        </div>
      </div>
    </div>
  )
}

export default QuickOpen
