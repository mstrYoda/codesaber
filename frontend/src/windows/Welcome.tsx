import React, { useCallback, useEffect, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { Recent } from '../../bindings/codesaber/backend/project/models'

const nameOf = (root: string) => root.slice(Math.max(root.lastIndexOf('/'), root.lastIndexOf('\\')) + 1) || root

const timeAgo = (iso: string): string => {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ''
  const mins = Math.round((Date.now() - t) / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.round(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  if (days < 30) return `${days}d ago`
  return new Date(iso).toLocaleDateString()
}

// Flow shared by Open Folder… and recents entries: open the project in the
// backend (emits project.added), bring up the workspace window, close welcome.
const openRoot = async (root: string) => {
  await App.OpenProject(root)
  await App.EnsureWorkspaceWindow()
  await App.CloseWelcome()
}

const Welcome: React.FC = () => {
  const [recents, setRecents] = useState<Recent[]>([])
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    setRecents((await App.RecentProjects()) ?? [])
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const openFolder = useCallback(async () => {
    if (busy) return
    setBusy(true)
    try {
      const root = await App.PickFolder()
      if (root) await openRoot(root)
    } finally {
      setBusy(false)
    }
  }, [busy])

  const openRecent = useCallback(
    (root: string) => {
      openRoot(root)
    },
    [],
  )

  const forget = useCallback(
    async (root: string) => {
      await App.ForgetRecent(root)
      refresh()
    },
    [refresh],
  )

  return (
    <div className="drag-region flex h-full bg-editor text-primary">
      {/* Left rail */}
      <div className="flex w-56 shrink-0 flex-col justify-center px-8 border-r border-panel bg-panel">
        <div className="mb-8">
          <div className="text-2xl font-semibold tracking-tight">codesaber</div>
          <div className="mt-1 text-xs text-dim">AI-native code editor</div>
        </div>
        <button
          onClick={openFolder}
          disabled={busy}
          className="no-drag w-full rounded px-3 py-2 text-sm text-white transition-colors disabled:opacity-60"
          style={{ backgroundColor: 'var(--accent)' }}
        >
          Open Folder…
        </button>
        <span
          className="mt-2 w-full cursor-not-allowed rounded px-3 py-2 text-sm text-dim"
          title="Coming in Phase 2"
        >
          New Project…
        </span>
        <span
          className="mt-2 w-full cursor-not-allowed rounded px-3 py-2 text-sm text-dim"
          title="Coming in Phase 2"
        >
          Clone Repo…
        </span>
      </div>
      {/* Right panel: recents */}
      <div className="no-drag flex-1 overflow-y-auto">
        {recents.length === 0 ? (
          <div className="flex h-full items-center justify-center px-8 text-center text-xs text-dim">
            No recent projects. Open a folder to get started.
          </div>
        ) : (
          <ul className="p-3">
            {recents.map((r) => (
              <li
                key={r.root}
                className="group flex cursor-pointer items-center justify-between rounded px-3 py-2 hover:bg-panel"
                onClick={() => openRecent(r.root)}
                title={r.root}
              >
                <div className="min-w-0">
                  <div className="truncate text-sm">{nameOf(r.root)}</div>
                  <div className="truncate text-[11px] text-dim">{r.root}</div>
                </div>
                <div className="ml-3 flex shrink-0 items-center gap-2 text-[11px] text-dim">
                  {r.branch && (
                    <span className="px-2 rounded bg-[#1e1f22]">
                      {'\u2387'} {r.branch}
                    </span>
                  )}
                  <span>{timeAgo(r.lastUsed)}</span>
                  <button
                    className="hidden rounded px-1 group-hover:block hover:text-primary"
                    title="Remove from recent list"
                    onClick={(e) => {
                      e.stopPropagation()
                      forget(r.root)
                    }}
                  >
                    {'\u2715'}
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

export default Welcome
