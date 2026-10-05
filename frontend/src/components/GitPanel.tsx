import React, { useEffect, useRef, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { ChangeStatus, DiffHunk } from '../../bindings/codesaber/backend/git/models'
import type { LogEntry } from '../../bindings/codesaber/backend/git/models'
import { useProjects } from '../state/projects'
import { useAgent } from '../state/agent'
import { useGit, diffKey } from '../state/git'
import { useTabs } from '../state/tabs'
import { shortcut } from '../lib/platform'
import {
  isBinaryPath,
  humanizeSize,
  langChip,
} from '../lib/filetype'

const statusLetter = (s: ChangeStatus) =>
  s === 65 /* A */ ? 'A' : s === 68 /* D */ ? 'D' : s === 85 /* U */ ? 'U' : 'M'

const relOf = (root: string, path: string) =>
  root && (path.startsWith(root + '/') || path.startsWith(root + '\\'))
    ? path.slice(root.length + 1).replace(/\\/g, '/')
    : path

// Status letter badges: filled rounded chips with per-status colors.
const LETTER_BADGE: Record<string, { bg: string; fg: string }> = {
  M: { bg: '#d99a4e', fg: '#000000' },
  A: { bg: '#5dbb63', fg: '#000000' },
  D: { bg: '#e53c34', fg: '#ffffff' },
  U: { bg: '#4a4f55', fg: '#ffffff' },
}

interface Row {
  path: string
  rel: string
  letter: ChangeStatus
  staged: boolean
  untracked?: boolean
  size?: number
}

// timeAgo renders compact relative time for history entries.
const timeAgo = (iso: string) => {
  const t = Date.parse(iso)
  if (isNaN(t)) return iso
  const s = Math.max(1, Math.floor((Date.now() - t) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

const SectionCard: React.FC<{
  label: string
  count: number
  dotClass: string
  open: boolean
  onToggle: () => void
  onQuick: () => void
  quickTitle: string
  quickGlyph: string
  emptyText: string
  children: React.ReactNode
}> = ({
  label,
  count,
  dotClass,
  open,
  onToggle,
  onQuick,
  quickTitle,
  quickGlyph,
  emptyText,
  children,
}) => (
  <div className="rounded-lg border border-[#333639] bg-[#242629] mb-2 overflow-hidden">
    <div className="group flex items-center gap-2 px-3 py-2">
      <button
        className="text-dim hover:text-primary flex items-center"
        onClick={onToggle}
        title={open ? 'Collapse' : 'Expand'}
      >
        <span
          className={
            'inline-block transition-transform text-[10px] w-3 ' +
            (open ? 'rotate-90' : '')
          }
        >
          ›
        </span>
      </button>
      <span className={'w-2 h-2 rounded-full shrink-0 ' + dotClass} />
      <span className="uppercase tracking-wide text-[10px] text-dim font-semibold flex-1 select-none">
        {label}
      </span>
      <button
        className="text-dim hover:text-primary opacity-0 group-hover:opacity-100"
        title={quickTitle}
        onClick={onQuick}
      >
        {quickGlyph}
      </button>
      <span className="rounded-full bg-[#3a3c3f] text-[11px] px-2 h-5 flex items-center text-[#bcbec4] shrink-0">
        {count}
      </span>
    </div>
    {open && (
      <div className="pb-1">
        {count === 0 ? (
          <div className="pl-5 pr-3 h-7 flex items-center text-dim">{emptyText}</div>
        ) : (
          children
        )}
      </div>
    )}
  </div>
)

const FileRow: React.FC<{
  r: Row
  stats?: { a: number; d: number }
  onRowClick: (r: Row) => void
  onOpenDiff: (r: Row) => void
  onStageHunks: (r: Row, idx: number[]) => Promise<void>
  onUnstageHunks: (r: Row, idx: number[]) => Promise<void>
}> = ({ r, stats, onRowClick, onOpenDiff, onStageHunks, onUnstageHunks }) => {
  const base = r.rel.slice(r.rel.lastIndexOf('/') + 1)
  const dir = r.rel.slice(0, r.rel.length - base.length)
  const badge = LETTER_BADGE[statusLetter(r.letter)]
  const showSize = r.untracked && r.size !== undefined && (isBinaryPath(r.path) || r.size > 100 * 1024)
  const [expanded, setExpanded] = useState(false)
  const [hunks, setHunks] = useState<DiffHunk[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [hunkError, setHunkError] = useState<string | null>(null)
  const [rowErr, setRowErr] = useState<string | null>(null)
  const [doneIdx, setDoneIdx] = useState<Set<number>>(new Set())
  const { fetchDiff, diffs } = useGit()
  const { activeId } = useProjects()

  // Load hunks lazily on first expand via the provider's diff fetch/cache.
  useEffect(() => {
    if (!expanded || !activeId) return
    setHunkError(null)
    setLoading(true)
    fetchDiff(diffKey(activeId, r.path, r.staged))
  }, [expanded, activeId, r.path, r.staged, fetchDiff])

  useEffect(() => {
    if (!expanded || !activeId) return
    const patch = diffs[diffKey(activeId, r.path, r.staged)]
    if (patch) {
      setHunks(patch.hunks ?? [])
      setLoading(false)
    }
  }, [expanded, activeId, r.path, r.staged, diffs, diffs && Object.keys(diffs).length])

  const applyHunk = (idx: number) => {
    if (!activeId) return
    setHunkError(null)
    setRowErr(null)
    const fn = r.staged ? onUnstageHunks : onStageHunks
    fn(r, [idx])
      .then(() => setDoneIdx((prev) => new Set(prev).add(idx)))
      .catch((e) => setRowErr(String(e)))
  }

  const stageAll = () => {
    if (!activeId) return
    setHunkError(null)
    setRowErr(null)
    const idxs = (hunks ?? []).map((_, i) => i)
    const fn = r.staged ? onUnstageHunks : onStageHunks
    fn(r, idxs)
      .then(() => setDoneIdx(new Set(idxs)))
      .catch((e) => setRowErr(String(e)))
  }

  return (
    <div>
      <div
        className="flex items-center gap-2 pl-5 pr-3 h-7 text-xs cursor-pointer hover:bg-white/4"
        title={r.staged ? 'Click to unstage' : 'Click to stage'}
        onClick={() => onRowClick(r)}
      >
        {/* Chevron left of the language chip: toggles inline hunk list */}
        <button
          className="text-dim hover:text-primary shrink-0 w-3 flex items-center justify-center"
          title={expanded ? 'Collapse hunks' : 'Expand hunks'}
          onClick={(e) => {
            e.stopPropagation()
            setExpanded((o) => !o)
          }}
        >
          <span
            className={
              'inline-block transition-transform text-[10px] ' + (expanded ? 'rotate-90' : '')
            }
          >
            ›
          </span>
        </button>
        {langChip(r.rel)}
        <span
          className="truncate flex-1 min-w-0"
          title={r.rel}
          onClick={(e) => {
            e.stopPropagation()
            onOpenDiff(r)
          }}
        >
          {dir && <span className="text-dim">{dir}</span>}
          <span className="text-white hover:underline">{base}</span>
        </span>
        {showSize ? (
          <span className="font-mono text-[11px] tabular-nums text-dim shrink-0">
            {humanizeSize(r.size ?? 0)}
          </span>
        ) : (
          !isBinaryPath(r.path) && (
            <>
              {stats && stats.a > 0 && (
                <span className="font-mono text-[11px] tabular-nums text-[#5dbb63] shrink-0">
                  +{stats.a}
                </span>
              )}
              {stats && stats.d > 0 && (
                <span className="font-mono text-[11px] tabular-nums text-[#e53c34] shrink-0">
                  -{stats.d}
                </span>
              )}
            </>
          )
        )}
        <span
          className="w-5 h-5 rounded-[6px] text-[10px] font-semibold flex items-center justify-center shrink-0"
          style={{ backgroundColor: badge.bg, color: badge.fg }}
        >
          {statusLetter(r.letter)}
        </span>
      </div>
      {expanded && (
        <div className="pb-1 pl-8 pr-3">
          {loading && <div className="h-6 flex items-center text-dim">Loading hunks…</div>}
          {!loading && hunkError && (
            <div className="h-6 flex items-center text-[#ff9999]">{hunkError}</div>
          )}
          {!loading && rowErr && (
            <div className="h-6 flex items-center text-dim truncate" title={rowErr}>
              {rowErr}
            </div>
          )}
          {!loading && hunks !== null && hunks.length === 0 && (
            <div className="h-6 flex items-center text-dim">No hunks</div>
          )}
          {!loading &&
            (hunks ?? []).map((h, i) => (
              <HunkRow
                key={i}
                idx={i}
                hunk={h}
                staged={r.staged}
                done={doneIdx.has(i)}
                onApply={() => applyHunk(i)}
              />
            ))}
          {!loading && (hunks ?? []).length > 0 && (
            <div className="pt-1">
              <button
                className="text-[10px] text-dim hover:text-primary uppercase tracking-wide"
                title={r.staged ? 'Unstage all hunks' : 'Stage all hunks (whole file)'}
                onClick={stageAll}
              >
                All
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// HunkRow renders one @@ header with its per-section action button; success
// tints the row green after applying, errors show inline dim.
const HunkRow: React.FC<{
  idx: number
  hunk: DiffHunk
  staged: boolean
  done: boolean
  onApply: () => void
  err?: string | null
}> = ({ idx, hunk, staged, done, onApply, err }) => (
  <div
    className={
      'flex items-center gap-2 h-6 text-[11px] rounded px-1 ' +
      (done ? 'bg-[#1d3324]' : 'hover:bg-white/4')
    }
  >
    <span className="font-mono text-dim truncate flex-1 min-w-0" title={hunk.header}>
      {hunk.header}
    </span>
    {err && <span className="text-dim truncate max-w-[40%]">{err}</span>}
    <button
      className="text-dim hover:text-primary shrink-0"
      title={staged ? 'Unstage hunk' : 'Stage hunk'}
      onClick={onApply}
      disabled={done}
    >
      {done ? '✓' : staged ? 'Unstage hunk' : 'Stage hunk'}
    </button>
  </div>
)

const seq = (rows: { path: string }[]) =>
  rows.map((r) => r.path).join('\0')

const GitPanel: React.FC = () => {
  const { activeId, projects } = useProjects()
  const { status, errors, fetchDiff, stage, unstage, stageHunks, unstageHunks, commit, setError } =
    useGit()
  const { openDiffTab } = useTabs()
  const { state: agentState, generateCommit } = useAgent()
  const [branches, setBranches] = useState<string[] | null>(null)
  const [branchOpen, setBranchOpen] = useState(false)
  const [message, setMessage] = useState('')
  const [tab, setTab] = useState<'changes' | 'history'>('changes')
  const [log, setLog] = useState<LogEntry[] | null>(null)
  const [openCommit, setOpenCommit] = useState<string | null>(null)
  const [sync, setSync] = useState<{
    remote: string
    ahead: number
    behind: number
  } | null>(null)
  const [fetching, setFetching] = useState(false)
  const [pushErr, setPushErr] = useState<string | null>(null)
  const [aiBusy, setAiBusy] = useState(false)
  const [open, setOpen] = useState({
    staged: true,
    unstaged: true,
    untracked: true,
  })
  const [stats, setStats] = useState<Record<string, { a: number; d: number }>>({})
  const st = activeId ? status[activeId] : undefined
  const err = activeId ? errors[activeId] : undefined
  const project = projects.find((p) => p.id === activeId)
  const branchRef = useRef<HTMLDivElement | null>(null)
  const commitMenuRef = useRef<HTMLDivElement | null>(null)
  const [commitMenuOpen, setCommitMenuOpen] = useState(false)

  const staged = st?.staged ?? []
  const unstaged = st?.unstaged ?? []
  const untracked = st?.untracked ?? []

  const ag = activeId ? agentState[activeId] : undefined
  const agentRunning = ag?.status === 'idle' || ag?.status === 'thinking'

  const resetSide = () => {
    setBranches(null)
    setBranchOpen(false)
    setSync(null)
    setLog(null)
    setOpenCommit(null)
    setStats({})
    setPushErr(null)
    setCommitMenuOpen(false)
  }
  useEffect(resetSide, [activeId])

  useEffect(() => {
    if (!branchOpen) return
    App.GitBranches(activeId ?? '')
      .then(setBranches)
      .catch(() => setBranches([]))
  }, [activeId, branchOpen])

  // HEAD changes refresh history/sync after commits on the same branch too.
  useEffect(() => {
    if (!activeId) return
    let cancelled = false
    App.GitAheadBehind(activeId)
      .then((s) => { if (!cancelled) setSync(s?.remote ? { remote: s.remote, ahead: s.ahead, behind: s.behind } : null) })
      .catch(() => { if (!cancelled) setSync(null) })
    return () => { cancelled = true }
  }, [activeId, st?.branch, st?.head])

  // Recent-commits log: fetched for both tabs (cheap, capped at 20).
  useEffect(() => {
    if (!activeId) return
    let cancelled = false
    App.GitLog(activeId, 20)
      .then((l) => { if (!cancelled) setLog(l ?? []) })
      .catch(() => { if (!cancelled) setLog([]) })
    return () => { cancelled = true }
  }, [activeId, st?.branch, st?.head])

  // Per-file +/- stats: lazily fetched for visible rows in capped batches.
  // Binary paths are skipped entirely — they render a human size (untracked)
  // or nothing instead of the notorious +24828 line-count artifact.
  useEffect(() => {
    if (!activeId || tab !== 'changes') return
    const batch = (rows: Row[], staged: boolean) => {
      const missing = rows
        .filter((r) => !stats[r.path] && !isBinaryPath(r.path))
        .map((r) => r.path)
      for (let i = 0; i < missing.length; i += 25) {
        App.GitStats(activeId, missing.slice(i, i + 25), staged)
          .then((res) =>
            setStats((prev) => {
              const next = { ...prev }
              for (const [p, v] of Object.entries(res ?? {})) {
                if (v && v.length >= 2) next[p] = { a: v[0], d: v[1] }
              }
              return next
            }),
          )
          .catch(() => {})
      }
    }
    batch(
      staged.map((c) => ({ path: c.path, rel: '', letter: c.status, staged: true })),
      true,
    )
    batch(
      [...unstaged, ...untracked].map((c) => ({
        path: c.path,
        rel: '',
        letter: c.status,
        staged: false,
      })),
      false,
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    activeId,
    tab,
    seq(staged),
    seq(unstaged),
    seq(untracked),
  ])

  // Dropdown close-on-outside-clicks for branch + commit split menus.
  useEffect(() => {
    if (!branchOpen && !commitMenuOpen) return
    const onDoc = (e: MouseEvent) => {
      if (branchOpen && !branchRef.current?.contains(e.target as Node))
        setBranchOpen(false)
      if (commitMenuOpen && !commitMenuRef.current?.contains(e.target as Node))
        setCommitMenuOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [branchOpen, commitMenuOpen])

  const rel = (p: string) =>
    relOf(project?.root ? project.root : '', p)

  const toggle = (r: Row) => {
    if (!activeId) return
    if (r.staged) unstage(activeId, [r.path])
    else stage(activeId, [r.path])
  }

  // Hunk actions: rejections propagate for per-row inline error display.
  const stageSelHunks = (r: Row, idx: number[]) => {
    if (!activeId) return Promise.resolve()
    return stageHunks(activeId, r.path, idx)
  }
  const unstageSelHunks = (r: Row, idx: number[]) => {
    if (!activeId) return Promise.resolve()
    return unstageHunks(activeId, r.path, idx)
  }

  const openDiff = (r: Row) => {
    if (!activeId) return
    fetchDiff(diffKey(activeId, r.path, r.staged))
    openDiffTab(activeId, r.path, r.staged)
  }

  const doFetch = () => {
    if (!activeId || fetching) return
    setFetching(true)
    setPushErr(null)
    App.GitFetch(activeId)
      .then(() => setError(activeId, ''))
      .catch((e) => setError(activeId, String(e)))
      .finally(() => {
        setFetching(false)
        App.GitAheadBehind(activeId)
          .then((s) =>
            setSync(
              s?.remote
                ? { remote: s.remote, ahead: s.ahead, behind: s.behind }
                : null,
            ),
          )
          .catch(() => {})
      })
  }

  const doCheckout = (b: string) => {
    if (!activeId || b === st?.branch) return
    setBranchOpen(false)
    setError(activeId, '')
    App.GitCheckout(activeId, b)
      .then(() => setError(activeId, ''))
      .catch((e) => setError(activeId, String(e)))
      .finally(() => {
        App.GitStatus(activeId).catch(() => {})
        setLog(null)
      })
  }

  const doCommit = async (push: boolean) => {
    if (!activeId) return
    setCommitMenuOpen(false)
    setPushErr(null)
    commit(activeId, message.trim())
      .then(() => setMessage(''))
      .then(() => (push ? App.GitPush(activeId).catch((e) => setPushErr(String(e))) : undefined))
      .catch(() => {})
  }

  const runAI = () => {
    if (!activeId || !agentRunning || aiBusy) return
    const summary = staged.map((c) => `${c.path} (${statusLetter(c.status)})`).join(', ')
    setAiBusy(true)
    generateCommit(
      activeId,
      `generate a concise conventional commit message for the currently staged changes summarized as: ${summary}`,
    )
      .then((m) => {
        const first = m.split('\n').filter(Boolean)[0] ?? ''
        setMessage(first)
      })
      .catch(() => {})
      .finally(() => setAiBusy(false))
  }

  const dimAction =
    'no-drag flex items-center gap-1 text-dim hover:text-primary disabled:opacity-40 disabled:cursor-not-allowed'

  return (
    <div className="flex flex-col h-full text-xs bg-[#1a1b1e] px-3 py-3">
      {err && (
        <div className="mb-2 px-3 py-1 rounded-lg bg-[#5a1d1d] text-[#ff9999] border border-[#ff6b6b]/30">
          {err}
        </div>
      )}
      {/* Row 1: branch card + sync pill */}
      <div className="shrink-0 flex items-center gap-2 mb-2">
        <div ref={branchRef} className="relative flex-1 min-w-0">
          <button
            className="no-drag w-full flex items-center gap-2 h-9 px-3 rounded-lg border border-[#333639] bg-[#242629] overflow-hidden hover:border-[#4a4f55]"
            onClick={() => setBranchOpen((o) => !o)}
            title="Switch branch"
          >
            <span className="shrink-0 text-dim">✏️</span>
            <span className="truncate font-semibold text-white">
              {st?.branch || '(none)'}
            </span>
            {st?.branch && sync?.remote && (
              <span className="truncate shrink text-dim">{`${sync.remote}/${st.branch}`}</span>
            )}
            <span className="shrink-0 ml-auto text-dim">⌄</span>
          </button>
          {branchOpen && (
            <div className="absolute left-0 right-0 z-20 mt-1 min-w-[160px] max-h-48 overflow-auto rounded-lg border border-[#333639] bg-[#242629] shadow-lg">
              {(branches ?? []).map((b) => (
                <div
                  key={b}
                  className={
                    'px-3 py-1.5 cursor-pointer hover:bg-white/4 ' +
                    (b === st?.branch ? 'text-white' : 'text-dim')
                  }
                  onClick={() => doCheckout(b)}
                >
                  {b}
                </div>
              ))}
              {branches !== null && branches.length === 0 && (
                <div className="px-3 py-1.5 text-dim">(no branches)</div>
              )}
            </div>
          )}
        </div>
        {sync && (
          <button
            className="no-drag shrink-0 h-9 px-3 rounded-lg border border-[var(--accent)]/40 bg-[#242629] flex items-center gap-1.5 font-mono text-[11px] hover:border-[var(--accent)]"
            title="Fetch"
            onClick={doFetch}
          >
            {fetching && <span>⌛</span>}
            <span className="text-[#5dbb63] tabular-nums">↑{sync.ahead}</span>
            <span className="text-[#e53c34] tabular-nums">↓{sync.behind}</span>
          </button>
        )}
      </div>
      {/* Row 2: small actions */}
      <div className="shrink-0 flex items-center gap-4 mb-2">
        <button
          className={dimAction}
          onClick={doFetch}
          disabled={fetching}
        >
          <span>⤓</span> Fetch
        </button>
        <button className={dimAction} title="Coming soon" disabled>
          Stash
        </button>
        <button className={dimAction + ' ml-auto'} title="Coming soon" disabled>
          Create PR
        </button>
      </div>
      {/* Tab strip: segmented control, full width */}
      <div className="shrink-0 flex items-stretch gap-1 p-1 rounded-lg border border-[#333639] bg-[#242629] mb-2">
        <button
          className={
            'flex-1 flex items-center justify-center gap-1.5 px-3 h-7 rounded-md text-xs ' +
            (tab === 'changes'
              ? 'bg-[#333639] text-white'
              : 'text-dim hover:text-white')
          }
          onClick={() => setTab('changes')}
        >
          Changes
          <span className="rounded-full bg-[#3a3c3f] px-1.5 text-[10px] text-[#bcbec4]">
            {staged.length + unstaged.length + untracked.length}
          </span>
        </button>
        <button
          className={
            'flex-1 flex items-center justify-center gap-1.5 px-3 h-7 rounded-md text-xs ' +
            (tab === 'history'
              ? 'bg-[#333639] text-white'
              : 'text-dim hover:text-white')
          }
          onClick={() => setTab('history')}
        >
          History
          {(log?.length ?? 0) > 0 && (
            <span className="rounded-full bg-[#3a3c3f] px-1.5 text-[10px] text-[#bcbec4]">
              {log?.length}
            </span>
          )}
        </button>
        <button
          className="flex-1 flex items-center justify-center px-3 h-7 rounded-md text-xs text-[#4a4f55] cursor-not-allowed"
          title="Coming soon"
          disabled
        >
          Stashes
          <span className="ml-1.5 rounded-full bg-[#3a3c3f] px-1.5 text-[10px] text-[#4a4f55]">
            0
          </span>
        </button>
      </div>
      {tab === 'changes' ? (
        <div className="flex-1 min-h-0 overflow-y-auto">
          <SectionCard
            label="Staged changes"
            count={staged.length}
            dotClass="bg-[#5dbb63]"
            open={open.staged}
            onToggle={() => setOpen((o) => ({ ...o, staged: !o.staged }))}
            onQuick={() => activeId && staged.length > 0 && unstage(activeId, staged.map((c) => c.path))}
            quickTitle="Unstage all"
            quickGlyph="−"
            emptyText="No staged changes"
          >
            {staged.map((c) => (
              <FileRow
                key={c.path}
                r={{ path: c.path, rel: rel(c.path), letter: c.status, staged: true }}
                stats={stats[c.path]}
                onRowClick={toggle}
                onOpenDiff={openDiff}
                onStageHunks={stageSelHunks}
                onUnstageHunks={unstageSelHunks}
              />
            ))}
          </SectionCard>
          <SectionCard
            label="Changes"
            count={unstaged.length}
            dotClass="bg-[#d99a4e]"
            open={open.unstaged}
            onToggle={() => setOpen((o) => ({ ...o, unstaged: !o.unstaged }))}
            onQuick={() => activeId && unstaged.length > 0 && stage(activeId, unstaged.map((c) => c.path))}
            quickTitle="Stage all"
            quickGlyph="+"
            emptyText="No changes"
          >
            {unstaged.map((c) => (
              <FileRow
                key={c.path}
                r={{ path: c.path, rel: rel(c.path), letter: c.status, staged: false }}
                stats={stats[c.path]}
                onRowClick={toggle}
                onOpenDiff={openDiff}
                onStageHunks={stageSelHunks}
                onUnstageHunks={unstageSelHunks}
              />
            ))}
          </SectionCard>
          <SectionCard
            label="Untracked"
            count={untracked.length}
            dotClass="bg-[#8a8f98]"
            open={open.untracked}
            onToggle={() => setOpen((o) => ({ ...o, untracked: !o.untracked }))}
            onQuick={() => activeId && untracked.length > 0 && stage(activeId, untracked.map((c) => c.path))}
            quickTitle="Stage all"
            quickGlyph="+"
            emptyText="No untracked files"
          >
            {untracked.map((c) => (
              <FileRow
                key={c.path}
                r={{
                  path: c.path,
                  rel: rel(c.path),
                  letter: c.status,
                  staged: false,
                  untracked: true,
                  size: c.size,
                }}
                stats={stats[c.path]}
                onRowClick={toggle}
                onOpenDiff={openDiff}
                onStageHunks={stageSelHunks}
                onUnstageHunks={unstageSelHunks}
              />
            ))}
          </SectionCard>
          {/* Recent commits on branch */}
          <div className="mt-3 border-t border-[#333639] pt-2">
            <div className="flex items-center justify-between px-1 mb-2">
              <span className="uppercase tracking-wide text-[10px] text-dim font-semibold">
                Recent on {st?.branch || '?'}
              </span>
              <button
                className="text-[10px] text-[var(--accent)] uppercase tracking-wide hover:underline"
                onClick={() => setTab('history')}
              >
                View log
              </button>
            </div>
            {(log ? log.slice(0, 3) : []).map((e) => (
              <div key={e.hash} className="mb-2 px-1">
                <div className="flex items-center gap-2">
                  <span className="w-1.5 h-1.5 rounded-full bg-[#5dbb63] shrink-0" />
                  <span className="text-white truncate text-xs">
                    {e.message.split('\n')[0]}
                  </span>
                </div>
                <div className="pl-3.5 text-[11px] text-dim truncate">
                  {e.hash.slice(0, 8)} · {timeAgo(e.time)} · {e.author}
                </div>
              </div>
            ))}
            {log !== null && log.length === 0 && (
              <div className="px-1 text-dim">No commits yet</div>
            )}
          </div>
        </div>
      ) : (
        <HistoryView
          log={log}
          branch={st?.branch ?? ''}
          onOpen={() => setTab('changes')}
          openHash={openCommit}
          setOpenHash={setOpenCommit}
        />
      )}
      {/* Commit area */}
      <div className="shrink-0 border-t border-[#333639] pt-3 mt-1 flex flex-col gap-2">
        <button
          className="no-drag relative w-full h-9 rounded-lg bg-gradient-to-r from-[#5b3fd4] to-[#6f51e0] text-white text-xs font-medium flex items-center justify-center gap-2 disabled:grayscale disabled:opacity-50 disabled:cursor-not-allowed"
          disabled={!agentRunning || aiBusy}
          title={
            agentRunning
              ? `Generate commit message with AI (${shortcut('I')})`
              : 'start an agent session first'
          }
          onClick={runAI}
        >
          <span>✨</span>
          {aiBusy ? 'Generating…' : 'Generate Commit Message with AI'}
          <span className="absolute right-2 h-5 px-1.5 rounded bg-white/15 text-[10px] font-mono flex items-center">
            {shortcut('I')}
          </span>
        </button>
        <textarea
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          placeholder="feat: …"
          rows={3}
          className="no-drag w-full resize-none bg-[#1e1f22] border border-[#333639] rounded-lg p-3 font-mono text-[13px] text-primary outline-none focus:ring-1 focus:ring-[var(--accent)] placeholder:text-dim"
        />
        <div className="flex items-center justify-between text-[10px]">
          <span className="text-dim">
            {staged.length > 0 ? `✓ ${staged.length} staged` : 'nothing staged'}
          </span>
          <span
            className={
              message.length > 50 ? 'text-[var(--modified)] font-medium' : 'text-dim'
            }
          >
            {message.length} / 72
          </span>
        </div>
        {pushErr && (
          <div className="px-2 py-1 rounded-lg bg-[#5a1d1d] text-[#ff9999] border border-[#ff6b6b]/30">
            {pushErr}
          </div>
        )}
        <div className="flex items-center gap-2">
          <div ref={commitMenuRef} className="relative flex-1 flex">
            <button
              disabled={staged.length === 0 || !message.trim() || !activeId}
              onClick={() => void doCommit(false)}
              className="no-drag flex-1 h-9 px-3 rounded-l-lg bg-[#3b5bfd] text-white font-medium disabled:opacity-40 disabled:cursor-not-allowed"
            >
              ✓ Commit
            </button>
            <button
              disabled={staged.length === 0 || !message.trim() || !activeId}
              onClick={() => setCommitMenuOpen((o) => !o)}
              className="no-drag h-9 px-1.5 rounded-r-lg border border-l-0 border-[#333639] bg-[#242629] text-dim hover:text-white disabled:opacity-40 disabled:cursor-not-allowed"
            >
              ▾
            </button>
            {commitMenuOpen && (
              <div className="absolute bottom-full mb-1 left-0 w-full min-w-[160px] rounded-lg border border-[#333639] bg-[#242629] shadow-lg z-20">
                <button
                  className="block w-full text-left px-3 py-1.5 text-xs text-dim hover:text-white hover:bg-white/4 disabled:opacity-40"
                  disabled={!message.trim()}
                  onClick={() => void doCommit(true)}
                >
                  Commit &amp; Push
                </button>
              </div>
            )}
          </div>
          <button
            className="no-drag h-9 px-3 rounded-lg text-dim opacity-50 cursor-not-allowed"
            title="Coming soon"
            disabled
          >
            Stash
          </button>
        </div>
      </div>
    </div>
  )
}

const HistoryView: React.FC<{
  log: LogEntry[] | null
  branch: string
  onOpen: () => void
  openHash: string | null
  setOpenHash: (h: string | null) => void
}> = ({ log, branch, onOpen, openHash, setOpenHash }) => (
  <div className="flex-1 min-h-0 overflow-y-auto">
    <div className="flex items-center justify-between px-1 mb-2">
      <span className="uppercase tracking-wide text-[10px] text-dim font-semibold">
        Recent on {branch}
      </span>
      <button
        className="text-[10px] text-[var(--accent)] uppercase tracking-wide hover:underline"
        onClick={onOpen}
      >
        View log
      </button>
    </div>
    {(log ?? []).map((e) => {
      const open = openHash === e.hash
      const first = e.message.split('\n')[0]
      return (
        <div
          key={e.hash}
          className="mb-2 px-1 py-1 rounded hover:bg-white/4 cursor-pointer"
          onClick={() => setOpenHash(open ? null : e.hash)}
        >
          <div className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-[#5dbb63] shrink-0" />
            <span className="text-white truncate text-xs">{first}</span>
          </div>
          <div className="pl-3.5 text-[11px] text-dim truncate">
            {e.hash.slice(0, 8)} · {timeAgo(e.time)} · {e.author}
          </div>
          {open && (
            <div className="mt-1 ml-3.5 rounded-lg bg-[#1e1f22] border border-[#333639] px-2 py-1.5 whitespace-pre-wrap text-dim text-[11px]">
              {e.message.trim() || first}
            </div>
          )}
        </div>
      )
    })}
    {log !== null && log.length === 0 && (
      <div className="px-1 py-2 text-dim">No commits</div>
    )}
    {log === null && <div className="px-1 py-2 text-dim">Loading…</div>}
  </div>
)

export default GitPanel
