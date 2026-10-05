import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react'
import { Events } from '@wailsio/runtime'
import * as App from '../../bindings/codesaber/backend/app'
import type { Buffer } from '../../bindings/codesaber/backend/editor/models'

export interface Tab {
  path: string
  title: string
  dirContent?: string
  buffer?: Buffer
  dirty: boolean
  staleExternally?: boolean
  kind?: 'file' | 'diff' | 'md-preview' | 'image'
  diffStaged?: boolean
  reveal?: { line: number; character: number }
}

export interface ProjectTabs {
  open: Tab[]
  active: string | null
}

interface TabsContextValue {
  tabsByProject: Record<string, ProjectTabs>
  saveError: string | null
  openFile: (
    projectId: string,
    path: string,
    opts?: { reveal?: { line: number; character: number } },
  ) => Promise<void>
  consumeReveal: (projectId: string, path: string) => void
  close: (projectId: string, path: string) => void
  setActive: (projectId: string, path: string) => void
  updateContent: (projectId: string, path: string, content: string) => void
  save: (projectId: string, path: string, content: string) => Promise<void>
  reload: (projectId: string, path: string, content: string) => void
  keepMine: (projectId: string, path: string) => void
  openDiffTab: (projectId: string, path: string, staged: boolean) => void
  openMdPreviewTab: (projectId: string, path: string) => void
  closeMdPreviewTab: (projectId: string, path: string) => void
}

const TabsContext = createContext<TabsContextValue | null>(null)

const titleOf = (path: string) => path.slice(Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) + 1) || path

// isImagePath reports whether a path looks like a raster image the app can
// preview inline (PNG/JPG/JPEG/GIF/WebP/BMP/SVG).
export const isImagePath = (path: string): boolean =>
  /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(path)

// diffTabPath encodes a diff tab's identity in its key so diff and file tabs
// can coexist in the strip without colliding on path.
export const diffTabPath = (path: string, staged: boolean) =>
  `\u0394:${staged ? 's' : 'u'}:${path}`

// parseDiffTabPath reverses diffTabPath (title prefix \u0394, then staged
// flag, then the real workspace-relative path).
export const parseDiffTabPath = (
  key: string,
): { path: string; staged: boolean } | null => {
  if (!key.startsWith('\u0394:')) return null
  const rest = key.slice(2)
  const colon = rest.indexOf(':')
  if (colon === -1) return null
  return { path: rest.slice(colon + 1), staged: rest.slice(0, colon) === 's' }
}

// mdPreviewTabPath encodes a markdown preview tab's identity in its key so
// preview and source tabs coexist in the strip without colliding on path.
export const mdPreviewTabPath = (path: string) => `\u25C7:${path}`

// parseMdPreviewTabPath reverses mdPreviewTabPath (prefix \u25C7, then the
// real workspace-relative path).
export const parseMdPreviewTabPath = (key: string): string | null =>
  key.startsWith('\u25C7:') ? key.slice(2) : null

// Window during which an fs.change echo for a path we just saved is ignored.
const saveEchoWindowMs = 2000
const bufferSyncMs = 300

const bufferKey = (projectId: string, path: string) => `${projectId}\0${path}`

export const TabsProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [tabsByProject, setTabsByProject] = useState<
    Record<string, ProjectTabs>
  >({})
  const [saveError, setSaveError] = useState<string | null>(null)
  const tabsRef = useRef(tabsByProject)
  const lastSaveRef = useRef<Record<string, number>>({})
  const errorTimerRef = useRef<number | undefined>(undefined)
  const bufferTimersRef = useRef(new Map<string, number>())
  const bufferTasksRef = useRef(new Map<string, Promise<unknown>>())
  const bufferIdsRef = useRef(new Map<string, string>())
  const openRequestsRef = useRef(new Map<string, symbol>())

  // Update the ref before scheduling React work: view cleanup and a fast
  // project switch must see the last keystroke, even before the next render.
  const updateTabs = useCallback(
    (fn: (prev: Record<string, ProjectTabs>) => Record<string, ProjectTabs>) => {
      const next = fn(tabsRef.current)
      tabsRef.current = next
      setTabsByProject(next)
    },
    [],
  )

  // Serialize a tab's backend operations, including close while the initial
  // read is still pending. Other files/projects have independent queues.
  const bufferTask = useCallback(<T,>(key: string, fn: () => Promise<T>): Promise<T> => {
    const prev = bufferTasksRef.current.get(key) ?? Promise.resolve()
    const task = prev.catch(() => {}).then(fn)
    bufferTasksRef.current.set(key, task)
    const cleanup = () => {
      if (bufferTasksRef.current.get(key) === task) bufferTasksRef.current.delete(key)
    }
    task.then(cleanup, cleanup)
    return task
  }, [])

  const syncBuffer = useCallback((projectId: string, path: string) => {
    const key = bufferKey(projectId, path)
    if (bufferTimersRef.current.has(key)) return
    bufferTimersRef.current.set(key, window.setTimeout(() => {
      bufferTimersRef.current.delete(key)
      const buffer = tabsRef.current[projectId]?.open.find((t) => t.path === path)?.buffer
      if (!buffer) return
      void bufferTask(key, () => App.BufferUpdate(projectId, path, buffer))
        .catch((e) => {
          const current = tabsRef.current[projectId]?.open.find((t) => t.path === path)
          if (current?.buffer?.id === buffer.id) {
            setSaveError(`Buffer sync failed: ${String(e)}`)
          }
        })
    }, bufferSyncMs))
  }, [bufferTask])

  const releaseBuffer = useCallback((projectId: string, path: string) => {
    const key = bufferKey(projectId, path)
    window.clearTimeout(bufferTimersRef.current.get(key))
    bufferTimersRef.current.delete(key)
    openRequestsRef.current.delete(key)
    void bufferTask(key, async () => {
      const id = bufferIdsRef.current.get(key)
      if (!id) return
      await App.BufferClose(projectId, path, id)
      bufferIdsRef.current.delete(key)
    }).catch((e) => setSaveError(`Buffer close failed: ${String(e)}`))
  }, [bufferTask])

  const mutateTab = useCallback(
    (
      projectId: string,
      path: string,
      patch: (t: Tab) => Partial<Tab>,
    ) => {
      updateTabs((prev) => {
        const st = prev[projectId]
        if (!st) return prev
        if (!st.open.some((t) => t.path === path)) return prev
        return {
          ...prev,
          [projectId]: {
            ...st,
            open: st.open.map((t) =>
              t.path === path ? { ...t, ...patch(t) } : t,
            ),
          },
        }
      })
    },
    [updateTabs],
  )

  const openFile = useCallback(
    async (
      projectId: string,
      path: string,
      opts?: { reveal?: { line: number; character: number } },
    ) => {
      const existing = tabsRef.current[projectId]?.open.find(
        (t) => t.path === path,
      )
      const needFetch = !existing || existing.dirContent == null
      updateTabs((prev) => {
        const st = prev[projectId] ?? { open: [], active: null }
        if (st.open.some((t) => t.path === path)) {
          return {
            ...prev,
            [projectId]: { ...st, active: path, open: st.open.map((t) => t.path === path ? { ...t, reveal: opts?.reveal } : t) },
          }
        }
        const tab: Tab = {
          path,
          title: titleOf(path),
          dirty: false,
          kind: isImagePath(path) ? 'image' : undefined,
          reveal: opts?.reveal,
        }
        return {
          ...prev,
          [projectId]: { open: [...st.open, tab], active: path },
        }
      })
      if (!needFetch) return
      const key = bufferKey(projectId, path)
      const request = Symbol()
      openRequestsRef.current.set(key, request)
      try {
        let buffer: Buffer | undefined
        let content: string
        if (isImagePath(path)) {
          content = await App.ReadFileB64(path)
        } else {
          buffer = await bufferTask(key, async () => {
            const b = await App.BufferRead(projectId, path)
            bufferIdsRef.current.set(key, b.id)
            return b
          })
          content = buffer.savedContent
        }
        if (openRequestsRef.current.get(key) !== request) return
        updateTabs((prev) => {
          const st = prev[projectId]
          if (!st) return prev
          return {
            ...prev,
            [projectId]: {
              ...st,
              open: st.open.map((t) =>
                t.path === path
                  ? {
                      ...t,
                      buffer,
                      dirContent: content,
                      dirty: !!buffer && buffer.content !== buffer.savedContent,
                    }
                  : t,
              ),
            },
          }
        })
      } catch {
        // leave tab without content; Task 10 editor will surface errors
      }
    },
    [bufferTask, updateTabs],
  )

  const close = useCallback((projectId: string, path: string) => {
    releaseBuffer(projectId, path)
    updateTabs((prev) => {
      const st = prev[projectId]
      if (!st) return prev
      const idx = st.open.findIndex((t) => t.path === path)
      if (idx === -1) return prev
      const open = st.open.filter((t) => t.path !== path)
      let active = st.active
      if (active === path) {
        active = open[Math.min(idx, open.length - 1)]?.path ?? null
      }
      return { ...prev, [projectId]: { open, active } }
    })
  }, [releaseBuffer, updateTabs])

  const consumeReveal = useCallback(
    (projectId: string, path: string) => {
      mutateTab(projectId, path, () => ({ reveal: undefined }))
    },
    [mutateTab],
  )

  const setActive = useCallback((projectId: string, path: string) => {
    updateTabs((prev) => {
      const st = prev[projectId]
      if (!st || !st.open.some((t) => t.path === path)) return prev
      return { ...prev, [projectId]: { ...st, active: path } }
    })
  }, [updateTabs])

  const updateContent = useCallback(
    (projectId: string, path: string, content: string) => {
      const buffer = tabsRef.current[projectId]?.open.find((t) => t.path === path)?.buffer
      if (!buffer || buffer.content === content) return
      mutateTab(projectId, path, () => ({
        buffer: { ...buffer, content, version: buffer.version + 1 },
        dirty: content !== buffer.savedContent,
      }))
      syncBuffer(projectId, path)
    },
    [mutateTab, syncBuffer],
  )

  const save = useCallback(
    async (projectId: string, path: string, content: string) => {
      const id = tabsRef.current[projectId]?.open.find((t) => t.path === path)?.buffer?.id
      try {
        await App.SaveFile(path, content)
        lastSaveRef.current[`${projectId}\0${path}`] = Date.now()
        mutateTab(projectId, path, (t) => {
          if (!t.buffer || t.buffer.id !== id) return {}
          return {
            buffer: { ...t.buffer, savedContent: content, version: t.buffer.version + 1 },
            dirty: t.buffer.content !== content,
            staleExternally: false,
            dirContent: content,
          }
        })
        syncBuffer(projectId, path)
        setSaveError(null)
      } catch (e) {
        setSaveError(`Save failed: ${String(e)}`)
        window.clearTimeout(errorTimerRef.current)
        errorTimerRef.current = window.setTimeout(
          () => setSaveError(null),
          4000,
        )
      }
    },
    [mutateTab, syncBuffer],
  )

  const reload = useCallback(
    (projectId: string, path: string, content: string) => {
      mutateTab(projectId, path, (t) => ({
        buffer: t.buffer
          ? { ...t.buffer, content, savedContent: content, version: t.buffer.version + 1 }
          : undefined,
        dirContent: content,
        dirty: false,
        staleExternally: false,
      }))
      syncBuffer(projectId, path)
    },
    [mutateTab, syncBuffer],
  )

  const keepMine = useCallback(
    (projectId: string, path: string) => {
      mutateTab(projectId, path, () => ({ staleExternally: false }))
    },
    [mutateTab],
  )

  // Diff tabs are never dirty: open is idempotent (activates an existing
  // diff tab); no file content is fetched here — the diff viewer pulls the
  // patch from the git provider by key.
  const openDiffTab = useCallback(
    (projectId: string, path: string, staged: boolean) => {
      const key = diffTabPath(path, staged)
      updateTabs((prev) => {
        const st = prev[projectId] ?? { open: [], active: null }
        if (st.open.some((t) => t.path === key)) {
          return { ...prev, [projectId]: { ...st, active: key } }
        }
        const tab: Tab = {
          path: key,
          title: `\u0394 ${titleOf(path)}`,
          dirty: false,
          kind: 'diff',
          diffStaged: staged,
        }
        return {
          ...prev,
          [projectId]: { open: [...st.open, tab], active: key },
        }
      })
    },
    [updateTabs],
  )

  // Markdown preview tabs mirror diff tabs: never dirty, open is idempotent
  // (activates an existing preview tab), content fetched once on creation.
  const openMdPreviewTab = useCallback(
    (projectId: string, path: string) => {
      const key = mdPreviewTabPath(path)
      const existing = tabsRef.current[projectId]?.open.find(
        (t) => t.path === key,
      )
      const needFetch = !existing || existing.dirContent == null
      updateTabs((prev) => {
        const st = prev[projectId] ?? { open: [], active: null }
        if (st.open.some((t) => t.path === key)) {
          return { ...prev, [projectId]: { ...st, active: key } }
        }
        const tab: Tab = {
          path: key,
          title: titleOf(path),
          dirty: false,
          kind: 'md-preview',
        }
        return {
          ...prev,
          [projectId]: { open: [...st.open, tab], active: key },
        }
      })
      if (!needFetch) return
      App.ReadFile(path)
        .then((content) => {
          updateTabs((prev) => {
            const st = prev[projectId]
            if (!st) return prev
            return {
              ...prev,
              [projectId]: {
                ...st,
                open: st.open.map((t) =>
                  t.path === key ? { ...t, dirContent: content } : t,
                ),
              },
            }
          })
        })
        .catch(() => {})
    },
    [updateTabs],
  )

  // Closing a preview tab returns focus to the source tab when the preview
  // was active, mirroring how the eye toggle reads as "leave preview".
  const closeMdPreviewTab = useCallback((projectId: string, path: string) => {
    const key = mdPreviewTabPath(path)
    updateTabs((prev) => {
      const st = prev[projectId]
      if (!st || !st.open.some((t) => t.path === key)) return prev
      const idx = st.open.findIndex((t) => t.path === key)
      const open = st.open.filter((t) => t.path !== key)
      let active = st.active
      if (active === key) {
        active = st.open.some((t) => t.path === path)
          ? path
          : (open[Math.min(idx, open.length - 1)]?.path ?? null)
      }
      return { ...prev, [projectId]: { open, active } }
    })
  }, [updateTabs])

  useEffect(() => {
    const off = Events.On('project.removed', (ev: any) => {
      const { id } = (ev.data ?? {}) as { id?: string }
      if (!id) return
      for (const tab of tabsRef.current[id]?.open ?? []) releaseBuffer(id, tab.path)
      updateTabs((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
    })
    return () => {
      off()
      // Flush pending snapshots when the workspace view is torn down. A
      // project switch leaves this provider mounted and needs no RPC wait.
      for (const [key, timer] of bufferTimersRef.current) {
        window.clearTimeout(timer)
        const [projectId, path] = key.split('\0')
        const buffer = tabsRef.current[projectId]?.open.find((t) => t.path === path)?.buffer
        if (buffer) void bufferTask(key, () => App.BufferUpdate(projectId, path, buffer)).catch(() => {})
      }
      bufferTimersRef.current.clear()
      window.clearTimeout(errorTimerRef.current)
    }
  }, [bufferTask, releaseBuffer, updateTabs])

  // fs.change reconcile: auto-reload non-dirty tabs, flag dirty tabs; the
  // editor surface renders the banner. remove/rename silently closes
  // non-dirty tabs. Immediately-after-save echoes are ignored so our own
  // save never triggers a reconcile against stale state.
  useEffect(() => {
    const off = Events.On('fs.change', (ev: any) => {
      const { projectId, path, op } = (ev.data ?? {}) as {
        projectId?: string
        path?: string
        op?: string
      }
      if (!projectId || !path) return
      const tab = tabsRef.current[projectId]?.open.find(
        (t) => t.path === path,
      )
      const previewKey = mdPreviewTabPath(path)
      const previewTab = tabsRef.current[projectId]?.open.find(
        (t) => t.path === previewKey,
      )
      if (!tab && !previewTab) return
      if (op === 'remove' || op === 'rename') {
        if (previewTab) close(projectId, previewKey)
        if (!tab) return
        if (!tab.dirty) close(projectId, path)
        return
      }
      const savedAt = lastSaveRef.current[`${projectId}\0${path}`]
      if (savedAt && Date.now() - savedAt < saveEchoWindowMs) return
      if (tab?.dirty) {
        mutateTab(projectId, path, () => ({ staleExternally: true }))
        return
      }
      const read = isImagePath(path)
        ? App.ReadFileB64(path)
        : App.ReadFile(path)
      read.then((content) => {
        const current = tabsRef.current[projectId]?.open.find((t) => t.path === path)
        if (tab && current) {
          if (current.buffer?.id !== tab.buffer?.id) return
          if (current.dirty || current.buffer?.version !== tab.buffer?.version) {
            mutateTab(projectId, path, () => ({ staleExternally: true }))
            return
          }
          reload(projectId, path, content)
        }
        if (previewTab) reload(projectId, previewKey, content)
      }).catch(() => {})
    })
    return () => off()
  }, [close, mutateTab, reload])

  return (
    <TabsContext.Provider
      value={{
        tabsByProject,
        saveError,
        openFile,
        close,
        consumeReveal,
        setActive,
        updateContent,
        save,
        reload,
        keepMine,
        openDiffTab,
        openMdPreviewTab,
        closeMdPreviewTab,
      }}
    >
      {children}
    </TabsContext.Provider>
  )
}

export const useTabs = (): TabsContextValue => {
  const ctx = useContext(TabsContext)
  if (!ctx) throw new Error('useTabs must be used within TabsProvider')
  return ctx
}
