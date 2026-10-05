// @vitest-environment jsdom
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ list: vi.fn(), install: vi.fn(), check: vi.fn(), login: vi.fn(), settings: vi.fn(), cancel: vi.fn() }))
vi.mock('../bindings/codesaber/backend/app', () => ({ ACPHarnesses: api.list, ACPInstall: api.install, ACPCheck: api.check, ACPLogin: api.login, ACPUseSeparateSettings: api.settings, ACPCancelInstall: api.cancel }))
import ProviderSetup from '../src/components/ProviderSetup'
let root: Root
const start = vi.fn()
const button = (text: string) => [...document.querySelectorAll('button')].find(b => b.textContent === text)!
beforeEach(async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  for (const fn of Object.values(api)) fn.mockReset()
  start.mockReset()
  api.list.mockResolvedValue([{ name: 'claude', available: false, managed: false, isolated: false }])
  document.body.innerHTML = '<div id="root"></div>'
  root = createRoot(document.getElementById('root')!)
})
afterEach(async () => { await act(async () => root.unmount()); vi.unstubAllGlobals() })
it('keeps missing providers selectable and enables start after installation', async () => {
  await act(async () => root.render(<ProviderSetup projectId="p" onStart={start} />))
  expect(button('Start').disabled).toBe(true)
  expect([...document.querySelectorAll('option')].every(o => !o.disabled)).toBe(true)
  expect([...document.querySelectorAll('option')].map(o => o.value)).toEqual(['claude', 'antigravity', 'opencode'])
  api.install.mockImplementation(async () => api.list.mockResolvedValue([{ name: 'claude', available: true, managed: true, isolated: false, version: '0.77.0' }]))
  await act(async () => button('Install provider').click())
  expect(api.install).toHaveBeenCalledWith('claude', false)
  expect(button('Start').disabled).toBe(false)
  expect(document.body.textContent).toContain('Managed version 0.77.0')
})
it('shows a failed installation and allows retry without a stuck loading state', async () => {
  api.install.mockRejectedValue(new Error('Download unavailable'))
  await act(async () => root.render(<ProviderSetup projectId="p" onStart={start} />))
  await act(async () => button('Install provider').click())
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('Download unavailable')
  expect(button('Install provider').disabled).toBe(false)
  expect(button('Start').disabled).toBe(true)
})
it('reports connection warnings without claiming that model access was tested', async () => {
  api.list.mockResolvedValue([{ name: 'claude', available: true }])
  api.check.mockResolvedValue({ ready: true, message: 'No model request was sent.', warnings: ['Local proxy unavailable'], model: 'MiniMax-M3' })
  await act(async () => root.render(<ProviderSetup projectId="p" onStart={start} />))
  await act(async () => button('Check connection').click())
  expect(document.body.textContent).toContain('No model request was sent.')
  expect(document.body.textContent).toContain('Local proxy unavailable')
  expect(document.body.textContent).toContain('MiniMax-M3')
  expect(start).not.toHaveBeenCalled()
})
it('handles a rejected start visibly instead of dropping an unhandled promise', async () => {
  api.list.mockResolvedValue([{ name: 'claude', available: true }])
  start.mockRejectedValue(new Error('Sign in required'))
  await act(async () => root.render(<ProviderSetup projectId="p" onStart={start} />))
  await act(async () => button('Start').click())
  expect(document.querySelector('[role="alert"]')?.textContent).toContain('Sign in required')
  expect(button('Start').disabled).toBe(false)
})
