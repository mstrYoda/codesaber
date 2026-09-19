import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
} from 'react'

type Modifier = 'mod' | 'shift' | 'alt'

type KeyboardShortcut = {
  key: string
  modifiers: Modifier[]
  handler: (event: KeyboardEvent) => void
  guard?: (event: KeyboardEvent) => boolean
}

// SHORTCUTS is the single source of truth for every key combo the app binds.
// Handlers stay in the components that own the state they act on, but the
// key/modifier assignment lives here so bindings never have to be hunted
// down file by file to see or change what's mapped to what.
export const SHORTCUTS = {
  commandPalette: { key: 'p', modifiers: ['mod', 'shift'], label: '⌘⇧P — Command palette' },
  quickOpenFiles: { key: 'p', modifiers: ['mod'], label: '⌘P — Quick open files' },
  quickOpenSymbols: { key: 't', modifiers: ['mod'], label: '⌘T — Quick open symbols' },
  inlineEdit: { key: 'k', modifiers: ['mod'], label: '⌘K — Inline AI edit' },
  toggleSidebar: { key: 'b', modifiers: ['mod'], label: '⌘B — Toggle files sidebar' },
  toggleTerminal: { key: 'j', modifiers: ['mod'], label: '⌘J — Toggle terminal' },
  toggleRightDock: { key: 'd', modifiers: ['mod'], label: '⌘D — Toggle source control dock' },
  perfHud: { key: 'h', modifiers: ['mod', 'shift'], label: '⌘⇧H — Perf HUD' },
  // Not consumed via useKeyboardShortcut: WorkspaceInner binds this key
  // itself (capture phase + native-menu coordination — see its ⌘W comment).
  // It's kept here so the combo is still visible in one place.
  closeTab: { key: 'w', modifiers: ['mod'], label: '⌘W — Close tab' },
} satisfies Record<string, { key: string; modifiers: Modifier[]; label: string }>

export type ShortcutId = keyof typeof SHORTCUTS

type KeyboardShortcutsContextValue = {
  register: (shortcut: KeyboardShortcut) => () => void
}

const KeyboardShortcutsContext =
  createContext<KeyboardShortcutsContextValue | null>(null)

function matchesShortcut(
  event: KeyboardEvent,
  shortcut: KeyboardShortcut,
) {
  const { key, modifiers } = shortcut

  const hasMod = event.metaKey || event.ctrlKey
  const hasShift = event.shiftKey
  const hasAlt = event.altKey

  const needsMod = modifiers.includes('mod')
  const needsShift = modifiers.includes('shift')
  const needsAlt = modifiers.includes('alt')

  return (
    event.key.toLowerCase() === key.toLowerCase() &&
    hasMod === needsMod &&
    hasShift === needsShift &&
    hasAlt === needsAlt
  )
}

export function KeyboardShortcutsProvider({
  children,
}: {
  children: React.ReactNode
}) {
  const shortcutsRef = useRef<KeyboardShortcut[]>([])

  const register = useCallback((shortcut: KeyboardShortcut) => {
    shortcutsRef.current.push(shortcut)

    return () => {
      shortcutsRef.current = shortcutsRef.current.filter(
        (registered) => registered !== shortcut,
      )
    }
  }, [])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const shortcut = [...shortcutsRef.current]
        .reverse()
        .find(
          (item) =>
            matchesShortcut(event, item) && (!item.guard || item.guard(event)),
        )

      if (!shortcut) return

      event.preventDefault()
      shortcut.handler(event)
    }

    window.addEventListener('keydown', handleKeyDown)

    return () => {
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [])

  const value = useMemo<KeyboardShortcutsContextValue>(
    () => ({
      register,
    }),
    [register],
  )

  return (
    <KeyboardShortcutsContext.Provider value={value}>
      {children}
    </KeyboardShortcutsContext.Provider>
  )
}

export function useKeyboardShortcut(
  id: ShortcutId,
  handler: (event: KeyboardEvent) => void,
  guard?: (event: KeyboardEvent) => boolean,
) {
  const context = useContext(KeyboardShortcutsContext)

  if (!context) {
    throw new Error(
      'useKeyboardShortcut must be used within KeyboardShortcutsProvider',
    )
  }

  const handlerRef = useRef(handler)
  handlerRef.current = handler

  const guardRef = useRef(guard)
  guardRef.current = guard

  useEffect(() => {
    const { key, modifiers } = SHORTCUTS[id]
    const shortcut: KeyboardShortcut = {
      key,
      modifiers,
      guard: guardRef.current && ((event) => guardRef.current!(event)),
      handler: (event) => {
        handlerRef.current(event)
      },
    }

    return context.register(shortcut)
  }, [context, id])
}