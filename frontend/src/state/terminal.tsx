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
import { getSettings } from '../lib/settings'
import { terminalLabel } from '../lib/platform'
import { useProjects } from './projects'

export interface TerminalTab {
  termId: string
  label: string
}

export interface ProjectTerminals {
  open: TerminalTab[]
  active: string | null
}

interface TerminalContextValue {
  terminalsByProject: Record<string, ProjectTerminals>
  open: () => Promise<void>
  close: (termId: string) => void
  setActive: (projectId: string, termId: string) => void
}

const TerminalContext = createContext<TerminalContextValue | null>(null)

// The PTY frames and inputs are raw bytes carried as base64 strings over
// JSON ([]byte marshals to base64).
export const bytesToB64 = (bytes: Uint8Array): string => {
  let s = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    s += String.fromCharCode(
      ...(bytes.subarray(i, Math.min(i + chunk, bytes.length)) as any),
    )
  }
  return btoa(s)
}

export const b64ToBytes = (b64: string): Uint8Array => {
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

export const TerminalProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const { activeId } = useProjects()
  const [terminalsByProject, setTerminalsByProject] = useState<
    Record<string, ProjectTerminals>
  >({})
  const terminalsRef = useRef(terminalsByProject)
  terminalsRef.current = terminalsByProject
  const activeIdRef = useRef(activeId)
  activeIdRef.current = activeId
  const seqRef = useRef(0)

  const open = useCallback(async () => {
    const projectId = activeIdRef.current
    if (!projectId) return
    const n = ++seqRef.current
    const suffix = `${n}-${crypto.randomUUID()}`
    const shell = getSettings().terminalShell
    try {
      const termId = await App.TermStart(
        projectId,
        shell,
        suffix,
      )
      setTerminalsByProject((prev) => {
        const st = prev[projectId] ?? { open: [], active: null }
        if (st.open.some((t) => t.termId === termId)) return prev
        return {
          ...prev,
          [projectId]: {
            open: [...st.open, { termId, label: terminalLabel(shell, n) }],
            active: termId,
          },
        }
      })
    } catch (e) {
      console.warn('TermStart failed:', e)
    }
  }, [])

  const remove = useCallback((projectId: string, termId: string) => {
    setTerminalsByProject((prev) => {
      const st = prev[projectId]
      if (!st) return prev
      const idx = st.open.findIndex((t) => t.termId === termId)
      if (idx === -1) return prev
      const open = st.open.filter((t) => t.termId !== termId)
      let active = st.active
      if (active === termId) {
        active = open[Math.min(idx, open.length - 1)]?.termId ?? null
      }
      return { ...prev, [projectId]: { open, active } }
    })
  }, [])

  const close = useCallback(
    (termId: string) => {
      const projectId = termId.split('|')[0] ?? ''
      remove(projectId, termId)
      App.TermStop(termId).catch(() => {})
    },
    [remove],
  )

  const setActive = useCallback((projectId: string, termId: string) => {
    setTerminalsByProject((prev) => {
      const st = prev[projectId]
      if (!st || !st.open.some((t) => t.termId === termId)) return prev
      return { ...prev, [projectId]: { ...st, active: termId } }
    })
  }, [])

  useEffect(() => {
    const off = Events.On('term.exit', (ev: any) => {
      const { projectId, termId } = (ev.data ?? {}) as {
        projectId?: string
        termId?: string
      }
      if (!projectId || !termId) return
      remove(projectId, termId)
    })
    return () => off()
  }, [remove])

  return (
    <TerminalContext.Provider
      value={{ terminalsByProject, open, close, setActive }}
    >
      {children}
    </TerminalContext.Provider>
  )
}

export const useTerminal = (): TerminalContextValue => {
  const ctx = useContext(TerminalContext)
  if (!ctx) throw new Error('useTerminal must be used within TerminalProvider')
  return ctx
}
