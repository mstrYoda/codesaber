// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const backend = vi.hoisted(() => ({
  activeId: 'one',
  events: new Map<string, Set<(ev: { data: unknown }) => void>>(),
  load: vi.fn(),
  sessions: vi.fn(),
  open: vi.fn(),
  clear: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, fn: (ev: { data: unknown }) => void) => {
      let handlers = backend.events.get(name)
      if (!handlers) {
        handlers = new Set()
        backend.events.set(name, handlers)
      }
      handlers.add(fn)
      return () => handlers.delete(fn)
    },
  },
}))
vi.mock('../src/state/projects', () => ({
  useProjects: () => ({ activeId: backend.activeId }),
}))
vi.mock('../bindings/codesaber/backend/app', () => ({
  ACPHarnesses: async () => [],
  ACPLoadTranscript: backend.load,
  ACPSessions: backend.sessions,
  ACPOpenSession: backend.open,
  ACPClearTranscript: backend.clear,
}))

import { AgentProvider, useAgent } from '../src/state/agent'
import AgentPanel from '../src/components/AgentPanel'

let root: Root
let agent: ReturnType<typeof useAgent>
const Probe = () => {
  agent = useAgent()
  return null
}
const workspace = () => (
  <React.StrictMode>
    <AgentProvider>
      <Probe />
      <AgentPanel />
    </AgentProvider>
  </React.StrictMode>
)
const transcript = (sessionID: string) => ({
  sessionID,
  entries: [{ role: 'user', text: `Message in ${sessionID}`, kind: 'text' }],
})
const session = (id: string) => ({
  id, projectId: 'one', title: id, created: '', updated: '', messageCount: 1,
})
const emit = (name: string, data: unknown) => {
  for (const fn of backend.events.get(name) ?? []) fn({ data })
}
const button = (text: string) =>
  Array.from(document.querySelectorAll('button')).find((el) => el.textContent?.includes(text))!
const click = async (text: string) => {
  await act(async () => button(text).click())
}
const mount = async () => {
  await act(async () => root.render(workspace()))
}
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

beforeEach(() => {
  vi.resetAllMocks()
  backend.activeId = 'one'
  backend.events.clear()
  backend.load.mockImplementation(async (id: string) => transcript(`${id}-latest`))
  backend.sessions.mockResolvedValue([session('one-latest'), session('one-older')])
  backend.open.mockImplementation(async (projectId: string, sessionID: string) => {
    emit('acp.transcript', { projectId, ...transcript(sessionID) })
  })
  backend.clear.mockResolvedValue(undefined)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  document.body.innerHTML = '<div id="root"></div>'
  root = createRoot(document.getElementById('root')!)
})

afterEach(async () => {
  await act(async () => root.unmount())
  vi.unstubAllGlobals()
})

describe('clearing the displayed agent transcript', () => {
  it('clears the older chat opened from history without selecting the latest chat', async () => {
    await mount()
    await click('History')
    const older = Array.from(document.querySelectorAll('span')).find((el) => el.textContent === 'one-older')!
    await act(async () => older.click())
    await click('Open')
    await click('Chat')
    expect(document.body.textContent).toContain('Message in one-older')

    backend.sessions.mockResolvedValue([session('one-latest')])
    await click('Clear transcript')

    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-older')
    expect(agent.state.one.timeline).toEqual([])
    expect(agent.activeSession.one).toBeNull()
    expect(agent.sessions.one.map((s) => s.id)).toEqual(['one-latest'])
    expect(button('Clear transcript').disabled).toBe(true)
    await click('Clear transcript')
    expect(backend.clear).toHaveBeenCalledTimes(1)
  })

  it('takes the target from the loaded transcript even when the session list differs', async () => {
    backend.load.mockResolvedValue(transcript('one-older'))
    await mount()
    expect(document.body.textContent).toContain('Message in one-older')
    await click('Clear transcript')
    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-older')
  })

  it('waits for the transcript event before changing the displayed session ID', async () => {
    backend.open.mockResolvedValue(undefined)
    await mount()
    await act(async () => agent.openSession('one', 'one-older'))
    expect(document.body.textContent).toContain('Message in one-latest')
    await click('Clear transcript')
    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-latest')
  })

  it('keeps another chat opened while clear is in flight', async () => {
    const pending = deferred<void>()
    backend.clear.mockReturnValue(pending.promise)
    await mount()
    await act(async () => agent.openSession('one', 'one-older'))
    await click('Clear transcript')
    await act(async () => agent.openSession('one', 'one-latest'))
    await act(async () => pending.resolve())

    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-older')
    expect(agent.activeSession.one).toBe('one-latest')
    expect(document.body.textContent).toContain('Message in one-latest')
    expect(button('Clear transcript').disabled).toBe(false)
  })

  it('keeps the transcript and surfaces a failed clear', async () => {
    backend.clear.mockRejectedValue(new Error('harness is running'))
    await mount()
    const before = agent.state.one
    await click('Clear transcript')
    expect(agent.state.one).toBe(before)
    expect(agent.activeSession.one).toBe('one-latest')
    expect(document.body.textContent).toContain('harness is running')
    expect(document.body.textContent).toContain('Message in one-latest')
  })

  it('does not clear before a transcript with an ID has loaded', async () => {
    const pending = deferred<ReturnType<typeof transcript>>()
    backend.load.mockReturnValue(pending.promise)
    await mount()
    expect(button('Clear transcript').disabled).toBe(true)
    await act(async () => agent.clearTranscript('one'))
    expect(backend.clear).not.toHaveBeenCalled()
  })

  it('ignores a disk read that finishes after an older chat is opened', async () => {
    const pending = deferred<ReturnType<typeof transcript>>()
    backend.load.mockReturnValue(pending.promise)
    await mount()
    await act(async () => agent.openSession('one', 'one-older'))
    await act(async () => pending.resolve(transcript('one-latest')))
    expect(document.body.textContent).toContain('Message in one-older')
    await click('Clear transcript')
    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-older')
  })

  it('does not restore a cleared transcript from a pending project reload', async () => {
    await mount()
    await act(async () => {
      backend.activeId = 'two'
      root.render(workspace())
    })
    const pending = deferred<ReturnType<typeof transcript>>()
    backend.load.mockReturnValue(pending.promise)
    await act(async () => {
      backend.activeId = 'one'
      root.render(workspace())
    })
    await click('Clear transcript')
    await act(async () => pending.resolve(transcript('one-latest')))
    expect(agent.activeSession.one).toBeNull()
    expect(agent.state.one.timeline).toEqual([])
    expect(agent.activeSession.two).toBe('two-latest')
    expect(button('Clear transcript').disabled).toBe(true)
  })

  it('keeps the target project when switching projects during clear', async () => {
    const pending = deferred<void>()
    backend.clear.mockReturnValue(pending.promise)
    await mount()
    await click('Clear transcript')
    await act(async () => {
      backend.activeId = 'two'
      root.render(workspace())
    })
    await act(async () => pending.resolve())
    expect(backend.clear).toHaveBeenCalledExactlyOnceWith('one', 'one-latest')
    expect(agent.activeSession.one).toBeNull()
    expect(agent.activeSession.two).toBe('two-latest')
    expect(document.body.textContent).toContain('Message in two-latest')
  })
})
