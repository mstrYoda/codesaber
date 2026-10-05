import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Events } from '@wailsio/runtime'
import * as App from '../../bindings/codesaber/backend/app'
import {
  extractSymbols,
  scoreSymbol,
  supportedExt,
  type Sym,
} from './symbolIndex'
import { useProjects } from './projects'

export interface SymHit extends Sym {
  projectId: string
  projectName: string
  root: string
  path: string
  rel: string
}

interface FileEntry {
  syms: Sym[]
  content: string
}

type ScanStatus = 'idle' | 'scanning' | 'ready'

interface SymbolsContextValue {
  status: ScanStatus
  progress: { done: number; total: number } | null
  stale: boolean
  count: number
  indexVersion: number
  scanAll: (force?: boolean) => Promise<void>
  ensureScanned: () => void
  query: (term: string) => SymHit[]
}

const SymbolsContext = createContext<SymbolsContextValue | null>(null)

const maxQueryResults = 60

export const SymbolsProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const { projects } = useProjects()
  const [status, setStatus] = useState<ScanStatus>('idle')
  const [progress, setProgress] = useState<{
    done: number
    total: number
  } | null>(null)
  const [stale, setStale] = useState(false)
  const [count, setCount] = useState(0)
  const [indexVersion, setIndexVersion] = useState(0)
  const entriesRef = useRef<Map<string, FileEntry>>(new Map())
  const hitsRef = useRef<SymHit[]>([])
  const scanningRef = useRef(false)
  const projectsRef = useRef(projects)
  projectsRef.current = projects
  const debounceRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(
    new Map(),
  )

  const rebuildHits = useCallback(() => {
    const out: SymHit[] = []
    for (const p of projectsRef.current) {
      for (const [key, entry] of entriesRef.current) {
        const [pid, path] = key.split('\0')
        if (pid !== p.id) continue
        for (const s of entry.syms) {
          out.push({
            ...s,
            projectId: p.id,
            projectName: p.name,
            root: p.root,
            path,
            rel: (path.startsWith(p.root + '/') || path.startsWith(p.root + '\\'))
              ? path.slice(p.root.length + 1)
              : path,
          })
        }
      }
    }
    hitsRef.current = out
    setCount(out.length)
    setIndexVersion((v) => v + 1)
  }, [])

  const scanAll = useCallback(
    async (force = false) => {
      if (scanningRef.current) return
      scanningRef.current = true
      setStatus('scanning')
      setProgress({ done: 0, total: 0 })
      try {
        const projectsNow = projectsRef.current
        const lists = await Promise.all(
          projectsNow.map((p) =>
            App.IndexFiles(p.root)
              .then((paths) => paths ?? [])
              .catch(() => [] as string[]),
          ),
        )
        const jobs: { projectId: string; path: string }[] = []
        for (let i = 0; i < projectsNow.length; i++) {
          for (const path of lists[i]) {
            if (!supportedExt(path)) continue
            jobs.push({ projectId: projectsNow[i].id, path })
          }
        }
        const total = jobs.length
        setProgress({ done: 0, total })
        let done = 0
        for (const job of jobs) {
          const key = job.projectId + '\0' + job.path
          try {
            const content = await App.ReadFile(job.path)
            const cached = entriesRef.current.get(key)
            if (force || !cached || cached.content !== content) {
              const syms = await extractSymbols(job.path, content)
              entriesRef.current.set(key, { syms, content })
            }
          } catch {
            entriesRef.current.delete(key)
          }
          done++
          if (done % 25 === 0 || done === total) {
            setProgress({ done, total })
          }
        }
        // Drop entries for files that no longer exist on disk.
        const live = new Set(jobs.map((j) => j.projectId + '\0' + j.path))
        for (const key of entriesRef.current.keys()) {
          if (!live.has(key)) entriesRef.current.delete(key)
        }
        setStale(false)
        rebuildHits()
        setStatus('ready')
      } finally {
        scanningRef.current = false
        setProgress(null)
      }
    },
    [rebuildHits],
  )

  const ensureScanned = useCallback(() => {
    if (scanningRef.current) return
    if (status === 'idle' || stale) void scanAll()
  }, [status, stale, scanAll])

  // fs.change reconcile: re-extract the touched file, drop removed files,
  // and flag the index stale on renames (new path unknown without a rescan).
  useEffect(() => {
    const runUpdate = (projectId: string, path: string, op?: string) => {
      const key = projectId + '\0' + path
      if (op === 'remove') {
        if (entriesRef.current.delete(key)) rebuildHits()
        return
      }
      if (op === 'rename') {
        setStale(true)
        return
      }
      if (!supportedExt(path)) return
      App.ReadFile(path)
        .then((content) =>
          extractSymbols(path, content).then((syms) => {
            entriesRef.current.set(key, { syms, content })
            rebuildHits()
          }),
        )
        .catch(() => {
          if (entriesRef.current.delete(key)) rebuildHits()
        })
    }
    const off = Events.On('fs.change', (ev: any) => {
      const { projectId, path, op } = (ev.data ?? {}) as {
        projectId?: string
        path?: string
        op?: string
      }
      if (!projectId || !path) return
      const known = entriesRef.current.has(projectId + '\0' + path)
      if (!known && !supportedExt(path)) return
      // Coalesce bursts (multi-file writes, saves) per path.
      const timers = debounceRef.current
      const prev = timers.get(path)
      if (prev) clearTimeout(prev)
      timers.set(
        path,
        setTimeout(() => {
          timers.delete(path)
          runUpdate(projectId, path, op)
        }, 250),
      )
    })
    return () => {
      off()
      for (const t of debounceRef.current.values()) clearTimeout(t)
      debounceRef.current.clear()
    }
  }, [rebuildHits])

  const query = useCallback((term: string): SymHit[] => {
    const scored: { hit: SymHit; score: number }[] = []
    const hits = hitsRef.current
    for (let i = 0; i < hits.length; i++) {
      const score = scoreSymbol(hits[i], term)
      if (score >= 0) scored.push({ hit: hits[i], score })
    }
    scored.sort(
      (a, b) =>
        b.score - a.score ||
        a.hit.name.length - b.hit.name.length ||
        (a.hit.name < b.hit.name ? -1 : 1) ||
        (a.hit.rel < b.hit.rel ? -1 : 1),
    )
    return scored.slice(0, maxQueryResults).map((s) => s.hit)
  }, [])

  const value = useMemo(
    () => ({
      status,
      progress,
      stale,
      count,
      indexVersion,
      scanAll,
      ensureScanned,
      query,
    }),
    [
      status,
      progress,
      stale,
      count,
      indexVersion,
      scanAll,
      ensureScanned,
      query,
    ],
  )

  return (
    <SymbolsContext.Provider value={value}>{children}</SymbolsContext.Provider>
  )
}

export const useSymbols = (): SymbolsContextValue => {
  const ctx = useContext(SymbolsContext)
  if (!ctx) throw new Error('useSymbols must be used within SymbolsProvider')
  return ctx
}
