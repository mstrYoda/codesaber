// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const backend = vi.hoisted(() => ({ pasteboardRead: vi.fn() }))
vi.mock('@wailsio/runtime', () => ({ Events: { On: () => () => {} } }))
vi.mock('../bindings/codesaber/backend/app', () => ({
  ListTree: () => Promise.resolve([]),
  PasteboardRead: backend.pasteboardRead,
}))
vi.mock('../src/state/projects', () => ({ useProjects: () => ({ setActive: vi.fn() }) }))
vi.mock('../src/state/tabs', () => ({ useTabs: () => ({ openFile: vi.fn() }) }))
import FileTree from '../src/components/FileTree'

let root: Root
beforeEach(async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  backend.pasteboardRead.mockReset().mockResolvedValue([])
  document.body.innerHTML = '<div id="root"></div>'
  root = createRoot(document.getElementById('root')!)
  await act(async () => root.render(<FileTree root="C:\\Test" projectId="test" />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

it.each(['input', 'textarea', 'contenteditable'])('leaves text paste in %s to the focused editor', async (kind) => {
  const editor = document.createElement(kind === 'contenteditable' ? 'div' : kind)
  if (kind === 'contenteditable') {
    editor.contentEditable = 'true'
    editor.tabIndex = 0
    // JSDOM has no content-editing implementation; expose the browser property.
    Object.defineProperty(editor, 'isContentEditable', { value: true })
  }
  document.body.append(editor)
  editor.focus()
  expect(document.activeElement).toBe(editor)
  const paste = new KeyboardEvent('keydown', { key: 'v', ctrlKey: true, bubbles: true, cancelable: true })
  await act(async () => { editor.dispatchEvent(paste) })
  expect(paste.defaultPrevented).toBe(false)
  expect(backend.pasteboardRead).not.toHaveBeenCalled()
})

it('still requests file paste outside a text editor', async () => {
  const paste = new KeyboardEvent('keydown', { key: 'v', ctrlKey: true, bubbles: true, cancelable: true })
  await act(async () => { window.dispatchEvent(paste) })
  expect(paste.defaultPrevented).toBe(true)
  expect(backend.pasteboardRead).toHaveBeenCalledOnce()
})
