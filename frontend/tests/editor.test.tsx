// @vitest-environment jsdom
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
  readFile: vi.fn(),
  didOpen: vi.fn(),
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
  ReadFile: backend.readFile,
  SaveFile: backend.save,
}))
vi.mock('../src/components/MdPreview', () => ({ default: () => null }))
vi.mock('../src/components/DiffViewer', () => ({ default: () => null }))
vi.mock('../src/lib/settings', () => ({
  bracketColorsEnabled: () => false,
  effectiveFontSize: () => 13,
  getSettings: () => ({ minimap: false }),
  onSettingsChange: () => () => {},
}))
vi.mock('../src/lsp', () => ({
  ensure: () => Promise.resolve(),
  didOpen: backend.didOpen,
  didChange: vi.fn(),
  didClose: vi.fn(),
  flushDidChange: vi.fn(),
}))

import { TabsProvider, useTabs } from '../src/state/tabs'
import { KeyboardShortcutsProvider } from '../src/state/keybinding'
import Editor from '../src/components/Editor'

let root: Root
let tabs: ReturnType<typeof useTabs>
const Probe = () => {
  tabs = useTabs()
  return null
}
const workspace = () => (
  <React.StrictMode>
    <KeyboardShortcutsProvider>
      <TabsProvider>
        <Probe />
        <Editor />
      </TabsProvider>
    </KeyboardShortcutsProvider>
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
const tab = (projectId = 'one', path = '/one/file.txt') =>
  tabs.tabsByProject[projectId].open.find((t) => t.path === path)!
const emit = (name: string, data: unknown) => {
  for (const fn of backend.events.get(name) ?? []) fn({ data })
}
const edit = async (content: string) => {
  await act(async () => {
    const v = view()
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: content } })
  })
}
const switchProject = async (id: string) => {
  await act(async () => {
    backend.activeId = id
    root.render(workspace())
  })
}

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
  backend.close.mockImplementation(async (projectId: string, path: string) => {
    backend.buffers.delete(`${projectId}\0${path}`)
  })
  backend.save.mockResolvedValue(undefined)
  backend.readFile.mockResolvedValue('external')
  backend.didOpen.mockResolvedValue(undefined)
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  // Layout is outside these tests; keep CodeMirror measurements off the
  // JSDOM animation queue while using the real editor state and DOM.
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

describe('project editor buffers', () => {
  it.each(['unsaved work', ''])('keeps %j across project switches without saving', async (content) => {
    await edit(content)
    await switchProject('two')
    await act(async () => tabs.openFile('two', '/two/file.txt'))
    await edit('other project')
    await switchProject('one')

    expect(view().state.doc.toString()).toBe(content)
    expect(tab().dirty).toBe(true)
    expect(tab('two', '/two/file.txt').buffer?.content).toBe('other project')
    expect(backend.save).not.toHaveBeenCalled()

    await act(async () => vi.advanceTimersByTimeAsync(300))
    expect(backend.buffers.get('one\0/one/file.txt')?.content).toBe(content)
    expect(backend.buffers.get('two\0/two/file.txt')?.content).toBe('other project')
  })

  it('saves the restored text and stays clean after another project switch', async () => {
    await edit('draft')
    await switchProject('two')
    await switchProject('one')
    await act(async () => tabs.save('one', '/one/file.txt', view().state.doc.toString()))
    await switchProject('two')
    await switchProject('one')
    expect(backend.save).toHaveBeenCalledWith('/one/file.txt', 'draft')
    expect(view().state.doc.toString()).toBe('draft')
    expect(tab().dirty).toBe(false)
  })

  it('keeps buffers separate when two projects open the same path', async () => {
    await edit('first project')
    await switchProject('two')
    await act(async () => tabs.openFile('two', '/one/file.txt'))
    await edit('second project')
    await switchProject('one')
    expect(view().state.doc.toString()).toBe('first project')
    await switchProject('two')
    expect(view().state.doc.toString()).toBe('second project')
  })

  it('reloads a clean inactive buffer after an external change', async () => {
    await switchProject('two')
    await act(async () => emit('fs.change', { projectId: 'one', path: '/one/file.txt', op: 'write' }))
    await switchProject('one')
    expect(view().state.doc.toString()).toBe('external')
    expect(tab().dirty).toBe(false)
    await edit('saved')
    expect(tab().dirty).toBe(true)
  })

  it('keeps edits made while a save is pending', async () => {
    let finish!: () => void
    backend.save.mockImplementation(() => new Promise<void>((resolve) => { finish = resolve }))
    await edit('snapshot')
    let saving!: Promise<void>
    await act(async () => { saving = tabs.save('one', '/one/file.txt', 'snapshot') })
    await edit('snapshot plus later work')
    await switchProject('two')
    await act(async () => { finish(); await saving })
    await switchProject('one')
    expect(view().state.doc.toString()).toBe('snapshot plus later work')
    expect(tab().buffer?.savedContent).toBe('snapshot')
    expect(tab().dirty).toBe(true)
  })

  it('keeps a dirty inactive buffer when the file changes externally', async () => {
    await edit('draft')
    await switchProject('two')
    await act(async () => emit('fs.change', { projectId: 'one', path: '/one/file.txt', op: 'write' }))
    await switchProject('one')
    expect(view().state.doc.toString()).toBe('draft')
    expect(tab().staleExternally).toBe(true)
    expect(backend.readFile).not.toHaveBeenCalled()
  })

  it('does not overwrite edits made while an external read is pending', async () => {
    let finish!: (text: string) => void
    backend.readFile.mockImplementation(() => new Promise<string>((resolve) => { finish = resolve }))
    await act(async () => emit('fs.change', { projectId: 'one', path: '/one/file.txt', op: 'write' }))
    await edit('draft')
    await act(async () => finish('external'))
    expect(view().state.doc.toString()).toBe('draft')
    expect(tab().staleExternally).toBe(true)
  })

  it('sends the restored Go buffer to LSP instead of the disk baseline', async () => {
    await act(async () => tabs.openFile('one', '/one/file.go'))
    await edit('package draft')
    await switchProject('two')
    backend.didOpen.mockClear()
    await switchProject('one')
    expect(backend.didOpen).toHaveBeenLastCalledWith('one', '/one/file.go', 'package draft')
  })

  it('restores an existing backend draft as dirty', async () => {
    backend.buffers.set('one\0/one/draft.txt', {
      id: 'restored', content: 'draft', savedContent: 'saved', version: 3,
    })
    await act(async () => tabs.openFile('one', '/one/draft.txt'))
    expect(view().state.doc.toString()).toBe('draft')
    expect(tab('one', '/one/draft.txt').dirty).toBe(true)
    await edit('saved')
    expect(tab('one', '/one/draft.txt').dirty).toBe(false)
  })

  it('finishes a pending read before closing and reopening the buffer', async () => {
    let finish!: (buffer: Buffer) => void
    const pending: Buffer = {
      id: 'pending', content: 'old draft', savedContent: 'saved', version: 1,
    }
    backend.read.mockImplementationOnce(() => new Promise<Buffer>((resolve) => { finish = resolve }))
    let opening!: Promise<void>
    await act(async () => { opening = tabs.openFile('one', '/one/pending.txt') })
    let reopening!: Promise<void>
    await act(async () => {
      tabs.close('one', '/one/pending.txt')
      reopening = tabs.openFile('one', '/one/pending.txt')
    })
    await act(async () => {
      backend.buffers.set('one\0/one/pending.txt', pending)
      finish(pending)
      await opening
      await reopening
    })
    expect(backend.close).toHaveBeenCalledWith('one', '/one/pending.txt', 'pending')
    expect(view().state.doc.toString()).toBe('saved')
    expect(tab('one', '/one/pending.txt').buffer?.id).not.toBe('pending')
  })

  it('releases closed buffers and opens a fresh disk baseline', async () => {
    const id = tab().buffer?.id
    await edit('draft')
    await act(async () => tabs.close('one', '/one/file.txt'))
    await act(async () => tabs.openFile('one', '/one/file.txt'))
    expect(view().state.doc.toString()).toBe('saved')
    expect(tab().buffer?.id).not.toBe(id)
    expect(backend.close).toHaveBeenCalledWith('one', '/one/file.txt', id)
  })

  it('drops project buffers and pending sync on project removal', async () => {
    await edit('draft')
    await act(async () => emit('project.removed', { id: 'one' }))
    await act(async () => vi.advanceTimersByTimeAsync(300))
    expect(tabs.tabsByProject.one).toBeUndefined()
    expect(backend.update).not.toHaveBeenCalled()
  })
})
