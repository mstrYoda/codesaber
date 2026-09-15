// Reactive wrapper around the backend settings store (RPC-backed).
// The full settings document is fetched once at module load; mutations are
// optimistic (local state updates immediately, SettingsPut persists async).
// bracketColors/perfHud keep a localStorage cache as a fallback so the very
// first paint (before the RPC resolves) matches the last session.
import { useSyncExternalStore } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { Model } from '../../bindings/codesaber/backend/settings/models'

export type Settings = Model

const KEY = 'codesaber.bracketColors'
const HUD_KEY = 'codesaber.perfHud'
const VIM_KEY = 'codesaber.vim'
export const SETTINGS_EVENT = 'codesaber:settings'

export const DEFAULTS: Settings = {
  bracketColors: true,
  minimap: true,
  terminalShell: '',
  editorFontSizePx: 0, // 0 = 13px
  searchIncludeGlobs: '',
  accentColor: '', // '' = #4a5bfc
  perfHud: false,
  vimMode: false,
}

// Effective editor font size in px (0 persisted = default 13).
export const effectiveFontSize = (s: Settings): number =>
  s.editorFontSizePx && s.editorFontSizePx >= 11 && s.editorFontSizePx <= 18
    ? s.editorFontSizePx
    : 13

// Effective accent color hex ('' persisted = default #4a5bfc).
export const effectiveAccent = (s: Settings): string =>
  /^#[0-9a-fA-F]{6}$/.test(s.accentColor ?? '') ? s.accentColor! : '#4a5bfc'

let state: Settings = { ...DEFAULTS }
const listeners = new Set<() => void>()

const emit = (): void => {
  for (const l of listeners) l()
  window.dispatchEvent(new Event(SETTINGS_EVENT))
}

// applyAccent mirrors the accent color onto the :root CSS var so the whole
// UI recolors live (removal falls back to the stylesheet default).
const applyAccent = (hex: string): void => {
  if (/^#[0-9a-fA-F]{6}$/.test(hex)) {
    document.documentElement.style.setProperty('--accent', hex)
  } else {
    document.documentElement.style.removeProperty('--accent')
  }
}

// Fetch the persisted document once; localStorage values act as the
// fallback until it resolves (and as the cache if the backend is down).
let started = false
const start = (): void => {
  if (started) return
  started = true
  try {
    state.bracketColors = window.localStorage.getItem(KEY) !== 'false'
    state.perfHud = window.localStorage.getItem(HUD_KEY) === 'true'
    state.vimMode = window.localStorage.getItem(VIM_KEY) === 'true'
  } catch {
    // storage unavailable — defaults apply
  }
  App.SettingsGet()
    .then((m) => {
      if (!m) return
      state = { ...DEFAULTS, ...m }
      applyAccent(state.accentColor ?? '')
      emit()
    })
    .catch(() => {})
}
start()

export const getSettings = (): Settings => {
  start()
  return state
}

const subscribe = (fn: () => void): (() => void) => {
  start()
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

// updateSettings applies a patch optimistically, mirrors toggle caches into
// localStorage and persists the full document via SettingsPut.
export const updateSettings = (patch: Partial<Settings>): void => {
  state = { ...state, ...patch }
  try {
    if (patch.bracketColors !== undefined)
      window.localStorage.setItem(KEY, String(patch.bracketColors))
    if (patch.perfHud !== undefined)
      window.localStorage.setItem(HUD_KEY, String(patch.perfHud))
    if (patch.vimMode !== undefined)
      window.localStorage.setItem(VIM_KEY, String(patch.vimMode))
  } catch {
    // storage unavailable — setting just won't be cached
  }
  applyAccent(state.accentColor ?? '')
  emit()
  App.SettingsPut({
    bracketColors: state.bracketColors,
    minimap: state.minimap,
    terminalShell: state.terminalShell,
    editorFontSizePx: state.editorFontSizePx,
    searchIncludeGlobs: state.searchIncludeGlobs,
    accentColor: state.accentColor,
    perfHud: state.perfHud,
    vimMode: state.vimMode,
  }).catch(() => {})
}

// useSettings subscribes the component to the settings document and returns
// it plus the mutate helper.
export const useSettings = (): {
  settings: Settings
  update: (patch: Partial<Settings>) => void
} => {
  const settings = useSyncExternalStore(subscribe, getSettings)
  return { settings, update: updateSettings }
}

// ---- Legacy toggle helpers (kept for PerfHUD / palette / ⌘⇧H paths) ----

export const bracketColorsEnabled = (): boolean => getSettings().bracketColors

export const toggleBracketColors = (): boolean => {
  const next = !bracketColorsEnabled()
  updateSettings({ bracketColors: next })
  return next
}

export const perfHudEnabled = (): boolean => getSettings().perfHud ?? false

export const setPerfHud = (v: boolean): void => {
  updateSettings({ perfHud: v })
}

export const togglePerfHud = (): boolean => {
  const next = !perfHudEnabled()
  setPerfHud(next)
  return next
}

export const onSettingsChange = (fn: () => void): (() => void) => {
  window.addEventListener(SETTINGS_EVENT, fn)
  return () => window.removeEventListener(SETTINGS_EVENT, fn)
}
