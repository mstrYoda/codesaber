// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({ head: 'first', remote: '', log: vi.fn() }))
vi.mock('../src/state/projects', () => ({ useProjects: () => ({ activeId: 'p', projects: [{ id: 'p', root: 'C:\\Test' }] }) }))
vi.mock('../src/state/tabs', () => ({ useTabs: () => ({ openDiffTab: vi.fn() }) }))
vi.mock('../src/state/agent', () => ({ useAgent: () => ({ state: {}, generateCommit: vi.fn() }) }))
vi.mock('../src/state/git', () => ({
  diffKey: () => '',
  useGit: () => ({ status: { p: { head: state.head, branch: 'main', staged: [], unstaged: [], untracked: [] } }, errors: {} }),
}))
vi.mock('../bindings/codesaber/backend/app', () => ({
  GitLog: state.log,
  GitAheadBehind: async () => ({ remote: state.remote, ahead: 0, behind: 0 }),
}))
import GitPanel from '../src/components/GitPanel'

let root: Root
beforeEach(async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  state.head = 'first'; state.remote = ''
  state.log.mockReset().mockImplementation(async () => [{ hash: state.head, message: state.head, author: 'Test', time: new Date().toISOString() }])
  document.body.innerHTML = '<div id="root"></div>'
  root = createRoot(document.getElementById('root')!)
})
afterEach(async () => { await act(async () => root.unmount()); vi.unstubAllGlobals() })
it('refreshes history after a commit without changing branch', async () => {
  await act(async () => root.render(<GitPanel />))
  expect(document.body.textContent).toContain('first')
  state.head = 'second'
  await act(async () => root.render(<GitPanel />))
  expect(state.log).toHaveBeenCalledTimes(2)
  expect(document.body.textContent).toContain('second')
  expect(document.body.textContent).not.toContain('first')
})
it('only displays a tracking branch when the backend reports one', async () => {
  await act(async () => root.render(<GitPanel />))
  expect(document.body.textContent).not.toContain('origin/main')
  state.remote = 'origin'; state.head = 'second'
  await act(async () => root.render(<GitPanel />))
  expect(document.body.textContent).toContain('origin/main')
})
