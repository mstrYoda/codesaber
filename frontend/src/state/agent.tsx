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
import type { Info as HarnessInfo } from '../../bindings/codesaber/backend/acp/models'
import type { SessionMeta } from '../../bindings/codesaber/backend/agentstore/models'
import { useProjects } from './projects'
import {
  emptyAgentState,
  reduceMsg,
  reduceTool,
  reducePermission,
  removePermission,
  reduceStateFlip,
  reduceTranscript,
  type AgentProjectState,
  type AgentState,
  type PermissionOption,
  type TranscriptEntry,
} from './agentTimeline'

export type { AgentState } from './agentTimeline'
export type {
  TimelineItem,
  MessageItem,
  ToolItem,
  PermissionItem,
  PermissionOption,
} from './agentTimeline'

interface AgentContextValue {
  state: Record<string, AgentProjectState>
  sessions: Record<string, SessionMeta[]>
  activeSession: Record<string, string | null>
  harnesses: HarnessInfo[]
  send: (projectId: string, text: string) => Promise<void>
  start: (projectId: string, harnessName: string) => Promise<void>
  stop: (projectId: string) => Promise<void>
  newSession: (projectId: string) => Promise<void>
  clearTranscript: (projectId: string) => Promise<void>
  respondPermission: (
    projectId: string,
    requestId: string,
    optionId: string,
    cancel: boolean,
  ) => Promise<void>
  listSessions: (projectId: string) => Promise<void>
  openSession: (projectId: string, sessionID: string) => Promise<void>
  deleteSession: (projectId: string, sessionID: string) => Promise<string | null>
  renameSession: (sessionID: string, title: string) => Promise<void>
  /** One-shot prompt turn with the last agent text captured (AI commit msg). */
  generateCommit: (projectId: string, prompt: string) => Promise<string>
}

const emptySessions: Record<string, SessionMeta[]> = {}

export const AgentProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [state, setState] = useState<Record<string, AgentProjectState>>({})
  const [sessions, setSessions] =
    useState<Record<string, SessionMeta[]>>(emptySessions)
  const activeSession = useMemo(
    () => Object.fromEntries(Object.entries(state).map(([id, s]) => [id, s.sessionId])),
    [state],
  )
  const transcriptVersion = useRef<Record<string, number>>({})
  const [harnesses, setHarnesses] = useState<HarnessInfo[]>([])

  const patch = useCallback(
    (projectId: string, fn: (s: AgentProjectState) => AgentProjectState) => {
      setState((prev) => ({
        ...prev,
        [projectId]: fn(prev[projectId] ?? emptyAgentState()),
      }))
    },
    [],
  )

  const refreshSessions = useCallback(async (projectId: string) => {
    try {
      const list = await App.ACPSessions(projectId)
      setSessions((prev) => ({ ...prev, [projectId]: list ?? [] }))
    } catch {
      setSessions((prev) => ({ ...prev, [projectId]: [] }))
    }
  }, [])

  useEffect(() => {
    const onMsg = Events.On('acp.msg', (ev: any) => {
      const { projectId, role, text, kind } = (ev.data ?? {}) as {
        projectId?: string
        role?: string
        text?: string
        kind?: string
      }
      if (!projectId || !text) return
      patch(projectId, (s) => reduceMsg(s, { role, text, kind }))
    })
    const onTool = Events.On('acp.tool', (ev: any) => {
      const { projectId, toolCallId, title, kind, status, content } =
        (ev.data ?? {}) as {
          projectId?: string
          toolCallId?: string
          title?: string
          kind?: string
          status?: string
          content?: string
        }
      if (!projectId || !toolCallId) return
      patch(projectId, (s) =>
        reduceTool(s, { toolCallId, title, kind, status, content }),
      )
    })
    const onPermission = Events.On('acp.permission', (ev: any) => {
      const {
        projectId,
        requestId,
        options,
        purpose,
        path,
        oldText,
        newText,
        isNew,
        truncated,
      } = (ev.data ?? {}) as {
        projectId?: string
        requestId?: string
        options?: unknown
        purpose?: string
        path?: string
        oldText?: string
        newText?: string
        isNew?: boolean
        truncated?: boolean
      }
      if (!projectId || !requestId) return
      const opts = (Array.isArray(options) ? options : [])
        .map(asPermissionOption)
        .filter(Boolean) as PermissionOption[]
      patch(projectId, (s) =>
        reducePermission(s, {
          type: 'permission',
          requestId,
          options: opts,
          purpose,
          path,
          oldText,
          newText,
          isNew,
          truncated,
        }),
      )
    })
    const onState = Events.On('acp.state', (ev: any) => {
      const { projectId, state: st } = (ev.data ?? {}) as {
        projectId?: string
        state?: AgentState
      }
      if (!projectId || !st) return
      patch(projectId, (s) => reduceStateFlip(s, st))
    })
    const onTranscript = Events.On('acp.transcript', (ev: any) => {
      const { projectId, sessionID, entries } = (ev.data ?? {}) as {
        projectId?: string
        sessionID?: string
        entries?: TranscriptEntry[]
      }
      if (!projectId) return
      transcriptVersion.current[projectId] = (transcriptVersion.current[projectId] ?? 0) + 1
      patch(projectId, (s) =>
        reduceTranscript({ ...s, sessionId: sessionID || null }, entries ?? []),
      )
      void refreshSessions(projectId)
    })
    const onRemoved = Events.On('project.removed', (ev: any) => {
      const { id } = (ev.data ?? {}) as { id?: string }
      if (!id) return
      transcriptVersion.current[id] = (transcriptVersion.current[id] ?? 0) + 1
      setState((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      setSessions((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
    })
    return () => {
      onMsg()
      onTool()
      onPermission()
      onState()
      onTranscript()
      onRemoved()
    }
  }, [patch, refreshSessions])

  const { activeId } = useProjects()
  useEffect(() => {
    App.ACPHarnesses()
      .then((hs) => setHarnesses(hs ?? []))
      .catch(() => setHarnesses([]))
  }, [])

  // Refresh sessions + pull the persisted transcript when the active project
  // changes so the panel shows history on open (the live `acp.transcript`
  // event only fires on backend-side transitions).
  useEffect(() => {
    if (!activeId) return
    const version = (transcriptVersion.current[activeId] ?? 0) + 1
    transcriptVersion.current[activeId] = version
    let cancelled = false
    void refreshSessions(activeId)
    App.ACPLoadTranscript(activeId)
      .then(({ sessionID, entries }) => {
        // A disk read must not replace a later selection or clear.
        if (cancelled || transcriptVersion.current[activeId] !== version) return
        patch(activeId, (s) =>
          reduceTranscript({ ...s, sessionId: sessionID || null }, entries ?? []),
        )
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [activeId, patch, refreshSessions])

  const send = useCallback(async (projectId: string, text: string) => {
    await App.ACPSendPrompt(projectId, text)
  }, [])

  const start = useCallback(
    async (projectId: string, harnessName: string) => {
      await App.ACPStart(projectId, harnessName)
      patch(projectId, (s) => ({ ...s, harness: harnessName }))
      void refreshSessions(projectId)
    },
    [patch, refreshSessions],
  )

  const stop = useCallback(
    async (projectId: string) => {
      await App.ACPStop(projectId)
      patch(projectId, (s) => ({ ...s, harness: null }))
    },
    [patch],
  )

  const newSession = useCallback(
    async (projectId: string) => {
      await App.ACPNewSession(projectId)
      // the backend emits an (empty) acp.transcript + sessions refresh after
      // spawning the fresh session
      void refreshSessions(projectId)
    },
    [refreshSessions],
  )

  const clearTranscript = useCallback(
    async (projectId: string) => {
      const sessionID = state[projectId]?.sessionId
      if (!sessionID) return
      await App.ACPClearTranscript(projectId, sessionID)
      transcriptVersion.current[projectId] = (transcriptVersion.current[projectId] ?? 0) + 1
      patch(projectId, (s) => s.sessionId === sessionID
        ? reduceTranscript({ ...s, sessionId: null }, [])
        : s)
      void refreshSessions(projectId)
    },
    [state, patch, refreshSessions],
  )

  const respondPermission = useCallback(
    async (
      projectId: string,
      requestId: string,
      optionId: string,
      cancel: boolean,
    ) => {
      await App.ACPRespondPermission(projectId, requestId, optionId, cancel)
      // resolve only the matching card, not the whole stack
      patch(projectId, (s) => removePermission(s, requestId))
    },
    [patch],
  )

  const listSessions = useCallback(
    async (projectId: string) => void refreshSessions(projectId),
    [refreshSessions],
  )

  const openSession = useCallback(
    async (projectId: string, sessionID: string) => {
      await App.ACPOpenSession(projectId, sessionID)
    },
    [],
  )

  const deleteSession = useCallback(
    async (projectId: string, sessionID: string): Promise<string | null> => {
      try {
        await App.ACPDeleteSession(projectId, sessionID)
        await refreshSessions(projectId)
        return null
      } catch (e) {
        return String(e)
      }
    },
    [refreshSessions],
  )

  const renameSession = useCallback(
    async (sessionID: string, title: string) => {
      await App.ACPRenameSession(sessionID, title)
      if (activeId) void refreshSessions(activeId)
    },
    [activeId, refreshSessions],
  )

  // generateCommit runs one synchronous prompt turn and resolves with the
  // LAST agent text captured via acp.msg. The turn is considered done when
  // the backend reports an idle acp.state (or on a 30s timeout). It listens
  // with local handlers so it never races the panel's own subscriptions.
  const generateCommit = useCallback(
    async (projectId: string, prompt: string): Promise<string> => {
      let captured = ''
      let done = false
      return new Promise<string>((resolve, reject) => {
        const fail = (why: string) => {
          if (done) return
          done = true
          offMsg()
          offState()
          clearTimeout(timer)
          reject(new Error(why))
        }
        const succeed = () => {
          if (done) return
          done = true
          offMsg()
          offState()
          clearTimeout(timer)
          const text = captured.trim()
          if (text) resolve(text)
          else reject(new Error('empty reply'))
        }
        const timer = setTimeout(() => fail('timeout'), 30000)
        const offMsg = Events.On('acp.msg', (ev: any) => {
          const { projectId: pid, role, text } = (ev.data ?? {}) as {
            projectId?: string
            role?: string
            text?: string
          }
          if (pid !== projectId || !text) return
          if (role === 'agent') captured = text
        })
        const offState = Events.On('acp.state', (ev: any) => {
          const { projectId: pid, state: st } = (ev.data ?? {}) as {
            projectId?: string
            state?: AgentState
          }
          if (pid !== projectId) return
          if (st === 'idle') succeed()
          else if (st === 'harness-down' || st === 'no-harness')
            fail('harness down')
        })
        App.ACPSendPrompt(projectId, prompt).catch((e) => fail(String(e)))
      })
    },
    [],
  )

  return (
    <AgentContext.Provider
      value={{
        state,
        sessions,
        activeSession,
        harnesses,
        send,
        start,
        stop,
        newSession,
        clearTranscript,
        respondPermission,
        listSessions,
        openSession,
        deleteSession,
        renameSession,
        generateCommit,
      }}
    >
      {children}
    </AgentContext.Provider>
  )
}

const AgentContext = createContext<AgentContextValue | null>(null)

// asPermissionOption coerces the flexible ACP permission option entries into
// the shape the panel renders.
const asPermissionOption = (o: unknown): PermissionOption | null => {
  if (typeof o !== 'object' || o === null) return null
  const m = o as Record<string, unknown>
  const optionId = typeof m.optionId === 'string' ? m.optionId : undefined
  const name = typeof m.name === 'string' ? m.name : undefined
  if (!optionId && !name) return null
  return {
    optionId,
    name,
    description: typeof m.description === 'string' ? m.description : undefined,
    kind: typeof m.kind === 'string' ? m.kind : undefined,
  }
}

export const useAgent = (): AgentContextValue => {
  const ctx = useContext(AgentContext)
  if (!ctx) throw new Error('useAgent must be used within AgentProvider')
  return ctx
}
