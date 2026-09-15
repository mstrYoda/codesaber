// @vitest-environment jsdom
// Verifies the vim Ex dialog's `:w` is wired to the app save path: typing
// `:w<Enter>` in the vim status panel must call App.SaveFile.
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { EditorView } from '@codemirror/view'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Buffer } from '../bindings/codesaber/backend/editor/models'

const backend = vi.hoisted(() => ({
  activeId: 'one',
  buffers: new Map<string, Buffer>(),
  events: new Map<string, Set<(ev: { data: unknown }) => void>>(),
  read: vi.fn(),
  update: vi.fn(),
  close: vi.fn(),
  save: vi.fn(),
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
vi.mock('../src/state/agent', () => ({
  useAgent: () => ({ generateCommit: vi.fn() }),
}))
vi.mock('../bindings/codesaber/backend/app', () => ({
  BufferRead: backend.read,
  BufferUpdate: backend.update,
  BufferClose: backend.close,
  SaveFile: backend.save,
}))
vi.mock('../src/components/MdPreview', () => ({ default: () => null }))
vi.mock('../src/components/DiffViewer', () => ({ default: () => null }))
vi.mock('../src/lib/settings', () => ({
  bracketColorsEnabled: () => false,
  effectiveFontSize: () => 13,
  getSettings: () => ({ minimap: false, vimMode: true }),
  onSettingsChange: () => () => {},
}))
vi.mock('../src/lsp', () => ({
  ensure: () => Promise.resolve(),
  didOpen: vi.fn(),
  didChange: vi.fn(),
  didClose: vi.fn(),
  flushDidChange: vi.fn(),
  didSave: vi.fn(() => Promise.resolve()),
}))

import { TabsProvider, useTabs } from '../src/state/tabs'
import Editor from '../src/components/Editor'

let root: Root
let tabs: ReturnType<typeof useTabs>
const Probe = () => {
  tabs = useTabs()
  return null
}
const workspace = () => (
  <React.StrictMode>
    <TabsProvider>
      <Probe />
      <Editor />
    </TabsProvider>
  </React.StrictMode>
)
const view = () => {
  const node = Array.from(document.querySelectorAll<HTMLElement>('.cm-editor')).find((el) => {
    for (let parent: HTMLElement | null = el; parent; parent = parent.parentElement) {
      if (parent.style.display === 'none') return false
    }
    return true
  })
  return EditorView.findFromDOM(node!)!
}
const emit = (name: string, data: unknown) => {
  for (const fn of backend.events.get(name) ?? []) fn({ data })
}
const key = (el: Element, type: string, opts: KeyboardEventInit = {}) =>
  el.dispatchEvent(new KeyboardEvent(type, { bubbles: true, cancelable: true, ...opts }))

beforeEach(async () => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  backend.activeId = 'one'
  backend.buffers.clear()
  backend.events.clear()
  backend.read.mockImplementation(async (projectId: string, path: string) => {
    const key = `${projectId}\0${path}`
    let buffer = backend.buffers.get(key)
    if (!buffer) {
      buffer = { id: crypto.randomUUID(), content: 'saved', savedContent: 'saved', version: 0 }
      backend.buffers.set(key, buffer)
    }
    return { ...buffer }
  })
  backend.update.mockImplementation(async (projectId: string, path: string, buffer: Buffer) => {
    backend.buffers.set(`${projectId}\0${path}`, { ...buffer })
  })
  backend.close.mockResolvedValue(undefined)
  backend.save.mockResolvedValue(undefined)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.spyOn(window, 'requestAnimationFrame').mockReturnValue(0)
  vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {})
  document.body.innerHTML = '<div id="root"></div>'
  root = createRoot(document.getElementById('root')!)
  await act(async () => root.render(workspace()))
  await act(async () => tabs.openFile('one', '/one/file.txt'))
})

afterEach(async () => {
  await act(async () => root.unmount())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('vim ex dialog save', () => {
  it('maps :w to the app save handler', async () => {
    await act(async () => {
      const v = view()
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: 'draft' } })
    })
    const contentDOM = view().contentDOM!
    await act(async () => {
      key(contentDOM, 'keydown', { key: ':' })
    })
    const input = document.querySelector('.cm-vim-panel input') as HTMLInputElement | null
    expect(input).toBeTruthy()
    input!.value = 'w'
    await act(async () => {
      key(input!, 'keydown', { key: 'Enter', keyCode: 13 })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50)
    })
    expect(backend.save).toHaveBeenCalledWith('/one/file.txt', 'draft')
  })
})
