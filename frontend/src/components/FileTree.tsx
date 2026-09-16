import React, { useEffect, useMemo, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as App from '../../bindings/codesaber/backend/app'
import type { Entry } from '../../bindings/codesaber/backend/models'
import type { PasteItem } from '../../bindings/codesaber/backend/models'
import { useProjects } from '../state/projects'
import { useTabs } from '../state/tabs'
import FileIcon from './FileIcon'
import ConfirmDialog from './ConfirmDialog'

const MAX_CHILDREN = 200

interface Row {
  key: string
  name: string
  path: string
  dir: boolean
  depth: number
  more?: boolean
}

interface Creating {
  parent: string
  depth: number
  folder?: boolean
}

interface CtxMenu {
  x: number
  y: number
  path: string | null
  dir: boolean
  pasteItems?: PasteItem[] | null
}

interface PendingDelete {
  path: string
  name: string
}

const parentOf = (p: string) => {
  const i = Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\'))
  if (i === 2 && p[1] === ':') return p.slice(0, 3)
  return i <= 0 ? p.slice(0, 1) : p.slice(0, i)
}

const FileTree: React.FC<{ root: string; projectId: string }> = ({
  root,
  projectId,
}) => {
  const { setActive } = useProjects()
  const { openFile } = useTabs()
  const [entries, setEntries] = useState<Entry[]>([])
  const [openDirs, setOpenDirs] = useState<Set<string>>(new Set())
  const [extra, setExtra] = useState<Record<string, Entry[]>>({})
  const [creating, setCreating] = useState<Creating | null>(null)
  const [draft, setDraft] = useState('')
  const [ctxMenu, setCtxMenu] = useState<CtxMenu | null>(null)
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const mountedRef = useRef(true)
  const openDirsRef = useRef(openDirs)
  openDirsRef.current = openDirs
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    if (!root) return
    let disposed = false
    setEntries([])
    setOpenDirs(new Set())
    setExtra({})
    App.ListTree(root).then((es) => {
      if (!disposed) setEntries(es ?? [])
    })
    return () => {
      disposed = true
    }
  }, [root])

  const depthOf = (p: string) =>
    p === root ? 0 : p.slice(root.length).split(/[\\/]/).filter(Boolean).length

  // refresh re-fetches the root listing plus any expanded deep dirs so
  // watcher-driven fs.change events keep the tree current.
  const refresh = () => {
    if (!mountedRef.current) return
    App.ListTree(root).then((es) => {
      if (mountedRef.current) setEntries(es ?? [])
    })
    for (const d of openDirsRef.current) {
      if (depthOf(d) < 2) continue
      App.ListTree(d).then((es) => {
        if (mountedRef.current)
          setExtra((prev) => ({ ...prev, [d]: es ?? [] }))
      })
    }
  }

  // fs.change → debounced refresh (watchers can burst during saves).
  useEffect(() => {
    if (!root) return
    let timer: number | undefined
    const off = Events.On('fs.change', (ev: any) => {
      const p = (ev.data ?? {}) as { path?: string }
      if (typeof p.path === 'string' && !p.path.startsWith(root)) return
      window.clearTimeout(timer)
      timer = window.setTimeout(refresh, 250)
    })
    return () => {
      off()
      window.clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root])

  // Sidebar "+" button → start creation at the project root.
  useEffect(() => {
    const onNew = (e: Event) => {
      if ((e as CustomEvent).detail?.projectId !== projectId) return
      startCreate(root)
    }
    window.addEventListener('codesaber:filetree-new', onNew)
    return () => window.removeEventListener('codesaber:filetree-new', onNew)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, root])

  const visibleRows = useMemo(() => {
    const rows: Row[] = []
    const emitted = new Set<string>()
    const closed: string[] = []
    const childCount: Record<string, number> = {}
    const push = (e: Entry) => {
      const parent = parentOf(e.path)
      const n = childCount[parent] ?? 0
      if (n >= MAX_CHILDREN) {
        if (n === MAX_CHILDREN) {
          childCount[parent] = n + 1
          rows.push({
            key: parent + '::more',
            name: '\u2026',
            path: parent + '::more',
            dir: false,
            depth: depthOf(e.path),
            more: true,
          })
        }
        return
      }
      childCount[parent] = n + 1
      rows.push({
        key: e.path,
        name: e.name,
        path: e.path,
        dir: e.dir,
        depth: depthOf(e.path),
      })
    }
    const visit = (e: Entry) => {
      if (emitted.has(e.path)) return
      if (closed.some((p) => e.path.startsWith(p + '/') || e.path.startsWith(p + '\\'))) return
      emitted.add(e.path)
      push(e)
      if (e.dir && !openDirs.has(e.path)) {
        closed.push(e.path)
      } else if (e.dir) {
        for (const c of extra[e.path] ?? []) visit(c)
      }
    }
    for (const e of entries) {
      if (e.path === root) continue
      visit(e)
    }
    return rows
  }, [entries, openDirs, extra, root])

  const toggle = (path: string) => {
    const wasOpen = openDirs.has(path)
    setOpenDirs((prev) => {
      const next = new Set(prev)
      if (wasOpen) next.delete(path)
      else next.add(path)
      return next
    })
    if (!wasOpen && depthOf(path) >= 2 && !extra[path]) {
      App.ListTree(path).then((es) => {
        if (mountedRef.current) setExtra((prev) => ({ ...prev, [path]: es ?? [] }))
      })
    }
  }

  // startCreate opens the inline name input under parent's children. A dir
  // parent is expanded first so the input appears in a visible spot.
  const startCreate = (parent: string, folder = false) => {
    if (parent !== root && !openDirsRef.current.has(parent)) {
      setOpenDirs((prev) => new Set(prev).add(parent))
      App.ListTree(parent).then((es) => {
        if (mountedRef.current)
          setExtra((prev) => ({ ...prev, [parent]: es ?? [] }))
      })
    }
    setDraft('')
    setCreating({ parent, depth: depthOf(parent) + 1, folder })
  }

  const cancelCreate = () => {
    setCreating(null)
    setDraft('')
  }

  // submitCreate: explicit folder intent (context menu) or a trailing "/"
  // makes a folder; otherwise a file (nested segments in the name create
  // intermediate dirs on the backend). A created file is opened in a tab
  // right away.
  const submitCreate = () => {
    if (!creating) return
    const raw = draft.trim()
    const folderIntent = creating.folder === true
    cancelCreate()
    if (!raw) return
    const isFolder = folderIntent || raw.endsWith('/')
    const clean = isFolder ? raw.replace(/\/+$/, '') : raw
    if (!clean) return
    const separator = creating.parent.includes('\\') ? '\\' : '/'
    const path = creating.parent.replace(/[\\/]+$/, '') + separator + clean.replace(/[\\/]/g, separator)
    const run = isFolder ? App.CreateFolder(path) : App.CreateFile(path)
    void run
      .then(() => {
        if (!isFolder) {
          setActive(projectId)
          void openFile(projectId, path)
        }
      })
      .catch((e) => {
        window.dispatchEvent(
          new CustomEvent('codesaber:status-hint', {
            detail: String(e).slice(0, 120),
          }),
        )
      })
  }

  useEffect(() => {
    if (creating) inputRef.current?.focus()
  }, [creating])

  // Context menu dismisses on any click elsewhere or window blur.
  useEffect(() => {
    if (!ctxMenu) return
    const dismiss = () => setCtxMenu(null)
    window.addEventListener('mousedown', dismiss)
    window.addEventListener('blur', dismiss)
    return () => {
      window.removeEventListener('mousedown', dismiss)
      window.removeEventListener('blur', dismiss)
    }
  }, [ctxMenu])

  // ⌘V anywhere in the tree pastes into the active project root.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey && !e.ctrlKey) return
      if (e.key.toLowerCase() !== 'v') return
      // Text controls, including CodeMirror's contenteditable surface, own paste.
      const el = document.activeElement
      if (
        el instanceof HTMLInputElement ||
        el instanceof HTMLTextAreaElement ||
        (el instanceof HTMLElement && el.isContentEditable)
      )
        return
      e.preventDefault()
      App.PasteboardRead()
        .then((items) => {
          if (items?.length)
            doPaste({ x: 0, y: 0, path: null, dir: true, pasteItems: items })
        })
        .catch(() => {})
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root])

  const openCtxMenu = (
    e: React.MouseEvent,
    path: string | null,
    dir: boolean,
  ) => {
    e.preventDefault()
    e.stopPropagation()
    // Probe the pasteboard so the Paste item can enable/disable itself;
    // a failed read just means Paste is greyed out.
    App.PasteboardRead()
      .then((items) => {
        setCtxMenu({ x: e.clientX, y: e.clientY, path, dir, pasteItems: items })
      })
      .catch(() => {
        setCtxMenu({ x: e.clientX, y: e.clientY, path, dir, pasteItems: null })
      })
  }

  // pasteTargetOf resolves where a paste should land: the folder itself,
  // the parent of a file, or the project root for empty-area clicks.
  const pasteTargetOf = (menu: CtxMenu): string =>
    menu.path ? (menu.dir ? menu.path : parentOf(menu.path)) : root

  const doPaste = (menu: CtxMenu) => {
    const items = menu.pasteItems ?? []
    if (!items.length) return
    const target = pasteTargetOf(menu)
    App.PasteInto(target, items)
      .then(() => refresh())
      .catch((e) => {
        window.dispatchEvent(
          new CustomEvent('codesaber:status-hint', {
            detail: String(e).slice(0, 120),
          }),
        )
      })
  }

  const deleteRow = (path: string) => {
    setPendingDelete({
      path,
      name: path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1) || path,
    })
  }

  const confirmDelete = () => {
    const target = pendingDelete
    setPendingDelete(null)
    if (!target) return
    App.DeletePath(target.path).catch((e) => {
      window.dispatchEvent(
        new CustomEvent('codesaber:status-hint', {
          detail: String(e).slice(0, 120),
        }),
      )
    })
  }

  const revealRow = (path: string) => {
    App.RevealInFinder(path).catch((e) => {
      window.dispatchEvent(
        new CustomEvent('codesaber:status-hint', {
          detail: String(e).slice(0, 120),
        }),
      )
    })
  }

  const onRowClick = (row: Row) => {
    if (row.more) return
    if (row.dir) toggle(row.path)
    else {
      setActive(projectId)
      void openFile(projectId, row.path)
    }
  }

  // insertIndexFor returns the visibleRows index after the last row inside
  // parent's subtree — where the inline create input should appear.
  const insertIndexFor = (parent: string) => {
    const prefix = parent + '/'
    let idx = -1
    visibleRows.forEach((r, i) => {
      if (r.path === parent || r.path.startsWith(prefix)) idx = i
    })
    return idx + 1
  }

  const rowEls = visibleRows.map((row) => (
    <div
      key={row.key}
      className="flex items-center gap-1 py-[3px] pr-2 hover:bg-[#373940] cursor-default"
      style={{ paddingLeft: 8 + row.depth * 12 }}
      onClick={() => onRowClick(row)}
      onContextMenu={(e) => openCtxMenu(e, row.path, row.dir)}
    >
      <span className="w-2 text-dim text-[9px]">
        {row.dir ? (openDirs.has(row.path) ? '\u25be' : '\u25b8') : ''}
      </span>
      <FileIcon path={row.path} dir={row.dir} open={openDirs.has(row.path)} />
      <span className={row.more || !row.dir ? 'text-dim' : 'text-primary'}>
        {row.name}
      </span>
    </div>
  ))

  if (creating) {
    rowEls.splice(insertIndexFor(creating.parent), 0, (
      <div
        key="::create"
        className="flex items-center py-[2px] pr-2"
        style={{ paddingLeft: 8 + creating.depth * 12 }}
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submitCreate()
            else if (e.key === 'Escape') cancelCreate()
          }}
          onBlur={cancelCreate}
          placeholder="name — trailing / for folder"
          spellCheck={false}
          className="w-full bg-[#1e1f22] border border-[var(--accent)] rounded px-1.5 py-[2px] text-[11px] text-primary outline-none"
        />
      </div>
    ))
  }

  return (
    <div
      className="py-1"
      onContextMenu={(e) => openCtxMenu(e, null, false)}
    >
      {rowEls}
      {visibleRows.length === 0 && !creating && (
        <div className="px-3 py-2 text-dim text-[10px]">Empty</div>
      )}
      {ctxMenu && (
        <div
          className="fixed z-50 min-w-[150px] rounded-md border border-[var(--bg-border)] bg-[var(--bg-panel)] shadow-lg py-1 text-[12px]"
          style={{ left: ctxMenu.x, top: ctxMenu.y }}
          // Mousedowns inside the menu must not reach the window-level
          // dismiss listener (it would unmount the menu before the click
          // lands). Actions run on click so the browser's mousedown default
          // focus behavior can't steal focus from a freshly-mounted inline
          // input.
          onMouseDown={(e) => e.stopPropagation()}
        >
          <button
            className="w-full text-left px-3 py-1.5 text-primary hover:bg-[#3b3d42]"
            onClick={() => {
              startCreate(ctxMenu.path ?? root, false)
              setCtxMenu(null)
            }}
          >
            New File
          </button>
          <button
            className="w-full text-left px-3 py-1.5 text-primary hover:bg-[#3b3d42]"
            onClick={() => {
              startCreate(ctxMenu.path ?? root, true)
              setCtxMenu(null)
            }}
          >
            New Folder
          </button>
          <button
            className={
              'w-full text-left px-3 py-1.5 hover:bg-[#3b3d42] ' +
              (ctxMenu.pasteItems?.length
                ? 'text-primary'
                : 'text-dim/60 cursor-default')
            }
            disabled={!ctxMenu.pasteItems?.length}
            title={
              ctxMenu.pasteItems?.length
                ? `Paste into ${
                    pasteTargetOf(ctxMenu) === root
                      ? 'project root'
                      : pasteTargetOf(ctxMenu).slice(root.length + 1)
                  }`
                : 'Nothing pasteable on the clipboard'
            }
            onClick={() => {
              doPaste(ctxMenu)
              setCtxMenu(null)
            }}
          >
            Paste
          </button>
          {ctxMenu.path && (
            <>
              <div className="my-1 h-px bg-[var(--bg-border)]" />
              <button
                className="w-full text-left px-3 py-1.5 text-primary hover:bg-[#3b3d42]"
                onClick={() => {
                  revealRow(ctxMenu.path!)
                  setCtxMenu(null)
                }}
              >
                Show in Finder
              </button>
              <button
                className="w-full text-left px-3 py-1.5 text-[#ff6b6b] hover:bg-[#3b3d42]"
                onClick={() => {
                  deleteRow(ctxMenu.path!)
                  setCtxMenu(null)
                }}
              >
                Delete
              </button>
            </>
          )}
        </div>
      )}
      {pendingDelete && (
        <ConfirmDialog
          title="Delete"
          message={`Delete "${pendingDelete.name}"? This cannot be undone.`}
          confirmLabel="Delete"
          danger
          onConfirm={confirmDelete}
          onCancel={() => setPendingDelete(null)}
        />
      )}
    </div>
  )
}

export default FileTree
