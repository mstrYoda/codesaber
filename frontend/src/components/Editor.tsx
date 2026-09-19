import React, {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'
import {
  EditorState,
  StateEffect,
  StateField,
  Compartment,
  RangeSetBuilder,
  type Extension,
} from '@codemirror/state'
import {
  EditorView,
  keymap,
  gutter,
  GutterMarker,
  hoverTooltip,
  showTooltip,
  scrollPastEnd,
  ViewPlugin,
  Decoration,
  type DecorationSet,
  type Tooltip,
  type ViewUpdate,
} from '@codemirror/view'
import { basicSetup } from 'codemirror'
import { StreamLanguage } from '@codemirror/language'
import { showMinimap } from '@replit/codemirror-minimap'
import { javascript } from '@codemirror/lang-javascript'
import { css } from '@codemirror/lang-css'
import { html } from '@codemirror/lang-html'
import { json } from '@codemirror/lang-json'
import { php } from '@codemirror/lang-php'
import { go } from '@codemirror/legacy-modes/mode/go'
import { darkSyntax } from '../lib/syntaxTheme'
import {
  bracketColorsEnabled,
  effectiveFontSize,
  getSettings,
  onSettingsChange,
} from '../lib/settings'
import { vimExtension, setVimSaveHandler } from '../lib/vim'
import { Events } from '@wailsio/runtime'
import { useProjects } from '../state/projects'
import { useTabs, type Tab } from '../state/tabs'
import { useKeyboardShortcut } from '../state/keybinding'
import DiffViewer from './DiffViewer'
import MdPreview from './MdPreview'
import * as App from '../../bindings/codesaber/backend/app'
import { useAgent } from '../state/agent'
import * as LSP from '../lsp'

// InlineKBarState is the floating ⌘K prompt bar: selection range it edits,
// screen coords (relative to the tab wrapper) for placement, and turn status.
interface InlineKBarState {
  from: number
  to: number
  left: number
  top: number
  status: 'input' | 'running' | 'done' | 'error'
  msg?: string
}

// imageMime maps an image file extension to its data-URL MIME subtype.
const imageMime = (path: string): string => {
  const name = path.slice(path.lastIndexOf('/') + 1).toLowerCase()
  if (name.endsWith('.jpg') || name.endsWith('.jpeg')) return 'jpeg'
  if (name.endsWith('.svg')) return 'svg+xml'
  const m = name.match(/\.(png|gif|webp|bmp)$/)
  return m ? m[1] : 'png'
}

const languageFor = (path: string): Extension => {
  const name = path.slice(path.lastIndexOf('/') + 1).toLowerCase()
  if (name.endsWith('.go')) return StreamLanguage.define(go)
  if (name.endsWith('.tsx')) return javascript({ typescript: true, jsx: true })
  if (name.endsWith('.jsx')) return javascript({ jsx: true })
  if (name.endsWith('.ts')) return javascript({ typescript: true })
  if (name.endsWith('.js') || name.endsWith('.mjs') || name.endsWith('.cjs'))
    return javascript()
  if (name.endsWith('.css')) return css()
  if (name.endsWith('.html') || name.endsWith('.htm')) return html()
  if (name.endsWith('.json')) return json()
  if (name.endsWith('.php')) return php()
  return []
}

const theme = EditorView.theme(
  {
    '&': {
      height: '100%',
      fontSize: '13px',
      backgroundColor: 'var(--bg-editor)',
      color: 'var(--text-primary)',
    },
    '.cm-scroller': {
      fontFamily: '"SF Mono", Menlo, Monaco, monospace',
      lineHeight: '1.5',
    },
    '.cm-content': { caretColor: 'var(--accent)' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)' },
    '.cm-gutters': {
      backgroundColor: 'var(--bg-editor)',
      color: '#4a4f55',
      border: 'none',
      paddingRight: '8px',
    },
    '.cm-activeLine': { backgroundColor: 'rgba(255, 255, 255, 0.045)' },
    '.cm-activeLineGutter': {
      backgroundColor: 'rgba(255, 255, 255, 0.045)',
      color: '#c8ccd2',
    },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': {
      backgroundColor: 'rgba(74, 91, 252, 0.30)',
    },
    '.cm-selectionMatch': { backgroundColor: 'rgba(74, 91, 252, 0.18)' },
    '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
      backgroundColor: 'rgba(74, 91, 252, 0.25)',
      outline: 'none',
      color: 'inherit',
    },
    '.cm-foldPlaceholder': {
      backgroundColor: 'var(--bg-panel)',
      border: '1px solid var(--bg-border)',
      color: 'var(--text-dim)',
    },
    '.cm-tooltip': {
      backgroundColor: 'var(--bg-panel)',
      border: '1px solid var(--bg-border)',
    },
    '.cm-panels': {
      backgroundColor: 'var(--bg-panel)',
      color: 'var(--text-primary)',
    },
    '& .cm-minimap-gutter': {
      backgroundColor: 'var(--bg-editor)',
      borderLeft: '1px solid var(--bg-border)',
      width: '62px !important',
    },
    '& .cm-minimap-inner': {
      backgroundColor: 'var(--bg-editor)',
    },
    '& .cm-minimap-inner canvas': {
      maxWidth: '60px !important',
    },
    '& .cm-minimap-overlay': {
      background: '#aecbff',
      opacity: 0.14,
    },
    '& .cm-minimap-overlay:hover, & .cm-minimap-overlay-active .cm-minimap-overlay':
      { opacity: 0.24 },
    '& .cm-minimap-box-shadow': {
      boxShadow: 'none',
    },
  },
  { dark: true },
)

// useBracketColors subscribes reactively to the persisted bracket
// colorization preference, so toggle updates reconfigure open views.
const useBracketColors = (): boolean =>
  useSyncExternalStore(onSettingsChange, bracketColorsEnabled)

// useMinimap / useFontSize track the settings-store minimap visibility and
// editor font size so changes reconfigure open views live.
const useMinimap = (): boolean =>
  useSyncExternalStore(onSettingsChange, () => getSettings().minimap)

const useFontSize = (): number =>
  useSyncExternalStore(onSettingsChange, () => effectiveFontSize(getSettings()))

const useVimMode = (): boolean =>
  useSyncExternalStore(onSettingsChange, () => !!getSettings().vimMode)

// minimapExt is the always-shaped minimap facet; visibility is toggled by
// wrapping it in a Compartment.
const minimapExt = showMinimap.compute(['doc'], () => ({
  create: (v: EditorView) => {
    const dom = document.createElement('div')
    return { dom }
  },
  displayText: 'characters',
  showOverlay: 'always',
}))

// fontTheme overrides the base theme's 13px font size (applied after `theme`
// in the extension list so the later style wins).
const fontTheme = (px: number): Extension =>
  EditorView.theme({ '&': { fontSize: `${px}px` } }, { dark: true })

// Bracket pair colorization with zero extra dependencies: a ViewPlugin
// scans the visible range (±200 lines) with a ()[]{} stack and marks each
// matched pair with a color by nesting depth (4-color cycle).
const BRACKET_COLORS = ['#d3739c', '#7aa2f7', '#56d4dd', '#aceebb']

const bracketTheme = EditorView.theme(
  Object.fromEntries(
    BRACKET_COLORS.map((c, i) => [
      `.cm-bracket-c${i}`,
      { color: `${c} !important` },
    ]),
  ),
)

const bracketDeco = BRACKET_COLORS.map((_, i) =>
  Decoration.mark({ class: `cm-bracket-c${i}` }),
)

class BracketNesting {
  decorations: DecorationSet
  constructor(view: EditorView) {
    this.decorations = this.build(view)
  }
  update(u: ViewUpdate) {
    if (u.docChanged || u.viewportChanged || u.geometryChanged) {
      this.decorations = this.build(u.view)
    }
  }
  build(view: EditorView): DecorationSet {
    const { state } = view
    if (!view.visibleRanges.length) return Decoration.none
    // widen scan to ±200 lines around the viewport so visible brackets
    // for pairs opening off-screen still get their depth color
    const firstLine = Math.max(
      1,
      state.doc.lineAt(view.visibleRanges[0].from).number - 200,
    )
    const lastLine = Math.min(
      state.doc.lines,
      state.doc.lineAt(view.visibleRanges[view.visibleRanges.length - 1].to)
        .number + 200,
    )
    const from = state.doc.line(firstLine).from
    const to = state.doc.line(lastLine).to
    const text = state.sliceDoc(from, to)
    const stack: { ch: string; pos: number }[] = []
    const pairs: { pos: number; depth: number }[] = []
    for (let i = 0; i < text.length; i++) {
      const ch = text[i]
      if (ch === '(' || ch === '[' || ch === '{') {
        stack.push({ ch, pos: i })
        continue
      }
      if (ch === ')' || ch === ']' || ch === '}') {
        const top = stack[stack.length - 1]
        if (
          top &&
          ((ch === ')' && top.ch === '(') ||
            (ch === ']' && top.ch === '[') ||
            (ch === '}' && top.ch === '{'))
        ) {
          stack.pop()
          const depth = stack.length % BRACKET_COLORS.length
          pairs.push({ pos: from + top.pos, depth }, { pos: from + i, depth })
        }
      }
    }
    pairs.sort((a, b) => a.pos - b.pos)
    const builder = new RangeSetBuilder<Decoration>()
    for (const p of pairs) builder.add(p.pos, p.pos + 1, bracketDeco[p.depth])
    return builder.finish()
  }
}

const bracketColors = ViewPlugin.fromClass(BracketNesting, {
  decorations: (v) => v.decorations,
})

const setDiagEffect = StateEffect.define<LSP.Diagnostic[] | null>()
const activeTipEffect = StateEffect.define<Tooltip | null>()

// diagsField keeps per-line diagnostics (line index → list) for the tab.
const diagsField = StateField.define<Map<number, LSP.Diagnostic[]>>({
  create: () => new Map(),
  update(lineDiags, tr) {
    let next = lineDiags
    for (const e of tr.effects) {
      if (e.is(setDiagEffect)) {
        next = new Map()
        if (e.value) {
          for (const d of e.value) {
            const arr = next.get(d.range.start.line) ?? []
            arr.push(d)
            next.set(d.range.start.line, arr)
          }
        }
      }
    }
    return next
  },
})

const tipField = StateField.define<Tooltip | null>({
  create: () => null,
  update(tip, tr) {
    for (const e of tr.effects) {
      if (e.is(activeTipEffect)) tip = e.value
      if (e.is(setDiagEffect)) tip = null
    }
    return tip
  },
  provide: (f) => showTooltip.from(f, (tip) => tip),
})

const sevColor = (severity?: number): string =>
  severity === 1
    ? '#c75454'
    : severity === 2
      ? '#bb9a3c'
      : '#4a91e2'

class DiagMarker extends GutterMarker {
  constructor(private color: string) {
    super()
  }
  toDOM(): Node {
    const dot = document.createElement('div')
    dot.className = 'cm-lsp-diag-dot'
    dot.style.background = this.color
    return dot
  }
}

const diagMarkers = new Map<string, DiagMarker>()
const markerFor = (severity?: number) => {
  const color = sevColor(severity)
  let m = diagMarkers.get(color)
  if (!m) {
    m = new DiagMarker(color)
    diagMarkers.set(color, m)
  }
  return m
}

const diagGutter = gutter({
  class: 'cm-lsp-diag-gutter',
  lineMarker: (view, line) => {
    const ds = view.state.field(diagsField).get(
      view.state.doc.lineAt(line.from).number - 1,
    )
    return ds?.length ? markerFor(ds[0].severity) : null
  },
  lineMarkerChange: () => true,
  domEventHandlers: {
    mousedown(view, line) {
      const ds = view.state.field(diagsField).get(
        view.state.doc.lineAt(line.from).number - 1,
      )
      if (!ds?.length) return false
      const dom = document.createElement('div')
      dom.className = 'cm-lsp-diag-tip'
      for (const d of ds) {
        const row = document.createElement('div')
        row.style.display = 'flex'
        row.style.gap = '6px'
        const dot = document.createElement('span')
        dot.className = 'cm-lsp-diag-dot'
        dot.style.background = sevColor(d.severity)
        dot.style.display = 'inline-block'
        dot.style.width = '7px'
        dot.style.height = '7px'
        dot.style.borderRadius = '50%'
        dot.style.marginTop = '5px'
        const text = document.createElement('span')
        text.textContent = d.message
        row.appendChild(dot)
        row.appendChild(text)
        dom.appendChild(row)
      }
      view.dispatch({
        effects: activeTipEffect.of({
          pos: line.from,
          create: () => ({ dom }),
        }),
      })
      return true
    },
  },
})

// modTracker tracks whether Mod is currently held (keydown/keyup on the
// editor DOM) so hover and goto-definition only react with Mod.
let modHeld = false
const modTracker = ViewPlugin.fromClass(
  class {
    private view: EditorView
    private onKey = (e: KeyboardEvent): void => {
      modHeld = e.metaKey || e.ctrlKey
    }
    constructor(view: EditorView) {
      this.view = view
      view.dom.addEventListener('keydown', this.onKey)
      view.dom.addEventListener('keyup', this.onKey)
    }
    destroy(): void {
      this.view.dom.removeEventListener('keydown', this.onKey)
      this.view.dom.removeEventListener('keyup', this.onKey)
    }
  },
)

// goHover turns a Mod-hover into a gopls hover tooltip (plain text).
const goHover = (projectId: string, path: string): Extension =>
  hoverTooltip(
    async (view, pos) => {
      if (!modHeld) return null
      const line = view.state.doc.lineAt(pos)
      const h = await LSP.hover(
        projectId,
        path,
        line.number - 1,
        pos - line.from,
      ).catch(() => null)
      const text = LSP.hoverText(h)
      if (!text) return null
      const dom = document.createElement('div')
      dom.className = 'cm-lsp-hover'
      dom.textContent = text
      return { pos, create: () => ({ dom }) }
    },
    { hideOnChange: true, hoverTime: 250 },
  )

// gotoOnModClick wires Mod+click → LSPDefinition → openFile(reveal).
const gotoOnModClick = (
  projectId: string,
  path: string,
  openFile: (p: string, path: string, opts?: { reveal?: { line: number; character: number } }) => Promise<void>,
): Extension =>
  EditorView.domEventHandlers({
    mousedown(event, view) {
      const mod = event.metaKey || event.ctrlKey
      if (!mod) {
        // clicking anywhere without Mod dismisses an open diag tooltip
        if (view.state.field(tipField, false))
          view.dispatch({ effects: activeTipEffect.of(null) })
        return false
      }
      const pos = view.posAtCoords({ x: event.clientX, y: event.clientY })
      if (pos == null) return false
      event.preventDefault()
      const line = view.state.doc.lineAt(pos)
      LSP.definition(projectId, path, line.number - 1, pos - line.from)
        .then((locs) => {
          const target = locs?.[0]
          if (!target || !target.uri) return
          return openFile(projectId, target.uri, {
            reveal: {
              line: target.range.start.line,
              character: target.range.start.character,
            },
          })
        })
        .catch(() => {})
      return true
    },
  })

// publishCursor emits the caret position for the status bar via a window
// CustomEvent, debounced 100ms (shared timer — latest position wins).
let cursorTimer: number | undefined
const publishCursor = (state: EditorState): void => {
  if (cursorTimer !== undefined) window.clearTimeout(cursorTimer)
  cursorTimer = window.setTimeout(() => {
    cursorTimer = undefined
    const pos = state.selection.main.head
    const line = state.doc.lineAt(pos)
    window.dispatchEvent(
      new CustomEvent('codesaber:cursor', {
        detail: { line: line.number, col: pos - line.from + 1 },
      }),
    )
  }, 100)
}

const lspTheme = EditorView.theme({  '.cm-lsp-diag-gutter': { width: '10px' },
  '.cm-lsp-diag-dot': {
    width: '7px',
    height: '7px',
    borderRadius: '50%',
    margin: '0 auto',
    display: 'inline-block',
  },
  '.cm-lsp-diag-tip, .cm-tooltip-hover': {
    backgroundColor: 'var(--bg-panel)',
    border: '1px solid var(--bg-border)',
    padding: '4px 8px',
    color: 'var(--text-primary)',
    fontSize: '12px',
    maxWidth: '420px',
    whiteSpace: 'pre-wrap',
  },
})

// InlineKBar is the floating prompt bar shown above the selection head.
// Enter dispatches the instruction (Esc closes); while the turn runs it
// shows a spinner, then 'applied' or the error, and closes on click.
const InlineKBar: React.FC<{
  st: InlineKBarState
  onSubmit: (instruction: string) => void
  onClose: () => void
}> = ({ st, onSubmit, onClose }) => {
  const [text, setText] = useState('')
  const inputRef = useRef<HTMLInputElement | null>(null)
  useEffect(() => inputRef.current?.focus(), [])

  const submit = () => {
    const t = text.trim()
    if (!t) return
    onSubmit(t)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      submit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  return (
    <div
      className="absolute z-40 flex items-center gap-2 rounded border border-[var(--accent)] bg-[var(--bg-panel)] shadow-lg px-2 py-1 text-xs no-drag"
      style={{
        left: st.left,
        top: st.top,
        transform: 'translateY(calc(-100% - 6px))',
        minWidth: 240,
      }}
    >
      {st.status === 'running' ? (
        <span className="text-dim animate-pulse">{'working\u2026'}</span>
      ) : st.status === 'done' || st.status === 'error' ? (
        <>
          <span
            className={
              st.status === 'done' ? 'text-[#7dcf9e]' : 'text-[#e5735f]'
            }
          >
            {st.msg ?? (st.status === 'done' ? 'applied' : 'failed')}
          </span>
          <button
            className="no-drag px-2 py-0.5 rounded bg-[#1e1f22] text-dim hover:text-primary"
            onClick={onClose}
          >
            close
          </button>
        </>
      ) : (
        <>
          <input
            ref={inputRef}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder={'edit selection\u2026'}
            className="flex-1 min-w-0 bg-transparent text-primary outline-none"
          />
          <button
            className="no-drag px-2 py-0.5 rounded bg-[#1e1f22] text-dim hover:text-primary"
            title="Esc"
            onClick={onClose}
          >
            esc
          </button>
        </>
      )}
    </div>
  )
}

const TabEditor: React.FC<{
  projectId: string
  tab: Tab
  active: boolean
}> = ({ projectId, tab, active }) => {
  const { updateContent, save, reload, keepMine, openFile, consumeReveal } =
    useTabs()
  const { generateCommit } = useAgent()
  const hostRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<EditorView | null>(null)
  const wrapRef = useRef<HTMLDivElement | null>(null)
  const openedRef = useRef(false)
  const onSaveRef = useRef(save)
  const onOpenFileRef = useRef(openFile)
  onSaveRef.current = save
  onOpenFileRef.current = openFile
  const isGo = tab.path.endsWith('.go')
  const bracketOn = useBracketColors()
  const bracketCompartmentRef = useRef(new Compartment())
  const minimapOn = useMinimap()
  const minimapCompartmentRef = useRef(new Compartment())
  const fontPx = useFontSize()
  const fontCompartmentRef = useRef(new Compartment())
  const vimOn = useVimMode()
  const vimCompartmentRef = useRef(new Compartment())
  const [inlineK, setInlineK] = useState<InlineKBarState | null>(null)
  const inlineKRef = useRef<InlineKBarState | null>(null)
  inlineKRef.current = inlineK
  const generateCommitRef = useRef(generateCommit)
  generateCommitRef.current = generateCommit

  // openInlineK arms the bar over the current selection. When the selection
  // is empty it no-ops with a brief status-bar hint instead.
  const openInlineK = useCallback(() => {
    if (inlineKRef.current?.status === 'running') return
    const view = viewRef.current
    const wrap = wrapRef.current
    if (!view || !wrap) return
    const sel = view.state.selection.main
    if (sel.empty) {
      window.dispatchEvent(
        new CustomEvent('codesaber:status-hint', {
          detail: 'select code to edit',
        }),
      )
      return
    }
    const head = view.coordsAtPos(sel.head)
    if (!head) return
    const r = wrap.getBoundingClientRect()
    const left = Math.max(
      8,
      Math.min(head.left - r.left, r.width - 280),
    )
    setInlineK({
      from: sel.from,
      to: sel.to,
      left,
      top: head.top - r.top,
      status: 'input',
    })
  }, [])

  // runInlineK composes the reduced-context prompt (rel path + line range +
  // selected text + instruction) and drives one synchronous ACP turn; the
  // agent's fs-write flow (with diff-review cards) handles applying it, and
  // the fs.change reconcile path reloads the file in place.
  const runInlineK = useCallback(
    (instruction: string) => {
      const st = inlineKRef.current
      const view = viewRef.current
      if (!st || !view) return
      const startLine = view.state.doc.lineAt(st.from).number
      const endLine = view.state.doc.lineAt(st.to).number
      const selected = view.state.sliceDoc(st.from, st.to)
      const prompt =
        `file: ${tab.path} (lines ${startLine}-${endLine})\n` +
        '```\n' +
        selected +
        '\n```\n' +
        `rewrite the above implementing: ${instruction}\n` +
        'Reply with the full new file if you can — or your diff if not possible (ACP agent protocol).'
      setInlineK((s) => (s ? { ...s, status: 'running' } : s))
      generateCommitRef
        .current(projectId, prompt)
        .then(() =>
          setInlineK((s) =>
            s ? { ...s, status: 'done', msg: 'applied' } : s,
          ),
        )
        .catch((e: unknown) => {
          const why = e instanceof Error ? e.message : String(e)
          const msg =
            why === 'timeout' ? 'timeout — check agent panel' : why.slice(0, 120)
          setInlineK((s) => (s ? { ...s, status: 'error', msg } : s))
        })
    },
    [projectId, tab.path],
  )

  // Cmd-K (or the File → AI: Edit Selection menu item) opens the bar; Escape
  // inside it just closes.
  useEffect(() => {
    if (!active) return
    const onMenu = () => openInlineK()
    window.addEventListener('codesaber:ai-edit', onMenu)
    return () => window.removeEventListener('codesaber:ai-edit', onMenu)
  }, [active, openInlineK])

  useKeyboardShortcut(
    'inlineEdit',
    openInlineK,
    useCallback(() => active, [active]),
  )

  useEffect(() => {
    if (!hostRef.current) return
    openedRef.current = false
    // one save path for both the Mod-s keymap and the vim Ex dialog (:w/:wq/:x)
    const doSave = (v: EditorView) => {
      const text = v.state.doc.toString()
      if (isGo) {
        LSP.flushDidChange(projectId, tab.path)
        onSaveRef.current?.(projectId, tab.path, text)
        LSP.didSave(projectId, tab.path, text).catch(() => {})
      } else {
        onSaveRef.current?.(projectId, tab.path, text)
      }
    }
    const view = new EditorView({
      state: EditorState.create({
        doc: tab.buffer?.content ?? '',
        extensions: [
          // diagnostics gutter goes before basicSetup so it renders to the
          // left of the line numbers
          diagsField,
          tipField,
          diagGutter,
          scrollPastEnd(),
          bracketCompartmentRef.current.of(bracketOn ? bracketColors : []),
          bracketTheme,
          basicSetup,
          theme,
          fontCompartmentRef.current.of(fontTheme(fontPx)),
          vimCompartmentRef.current.of(vimOn ? vimExtension() : []),
          darkSyntax,
          lspTheme,
          minimapCompartmentRef.current.of(minimapOn ? minimapExt : []),
          keymap.of([
            {
              key: 'Mod-s',
              preventDefault: true,
              run: (v) => {
                doSave(v)
                return true
              },
            },
          ]),
          languageFor(tab.path),
          modTracker,
          ...(isGo ? [goHover(projectId, tab.path), gotoOnModClick(projectId, tab.path, onOpenFileRef.current)] : []),
          EditorView.updateListener.of((u) => {
            if (u.selectionSet || u.docChanged) {
              publishCursor(u.state)
            }
            if (!u.docChanged) return
            if (isGo) {
              LSP.didChange(projectId, tab.path, u.state.doc.toString())
            }
            updateContent(projectId, tab.path, u.state.doc.toString())
          }),
        ],
      }),
      parent: hostRef.current,
    })
    viewRef.current = view
    setVimSaveHandler(view, () => doSave(view))
    return () => {
      if (openedRef.current) {
        LSP.flushDidChange(projectId, tab.path)
        LSP.didClose(projectId, tab.path)
      }
      view.destroy()
      viewRef.current = null
    }
    // Views may unmount on project switches; the provider owns the live text.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.path, projectId, updateContent])

  // toggle bracket colorization live via reconfigurable compartment
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: bracketCompartmentRef.current.reconfigure(
        bracketOn ? bracketColors : [],
      ),
    })
  }, [bracketOn])

  // toggle the minimap live via its compartment
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: minimapCompartmentRef.current.reconfigure(
        minimapOn ? minimapExt : [],
      ),
    })
  }, [minimapOn])

  // apply editor font size changes live via its compartment
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: fontCompartmentRef.current.reconfigure(fontTheme(fontPx)),
    })
  }, [fontPx])

  // toggle vim mode live via its compartment
  useEffect(() => {
    viewRef.current?.dispatch({
      effects: vimCompartmentRef.current.reconfigure(
        vimOn ? vimExtension() : [],
      ),
    })
  }, [vimOn])

  // didOpen once per tab once content exists (gopls must see full text).
  useEffect(() => {
    if (!isGo || openedRef.current || !tab.buffer) return
    LSP.ensure(projectId).catch(() => {})
    openedRef.current = true
    LSP.didOpen(projectId, tab.path, tab.buffer.content).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isGo, tab.buffer])

  // lsp.diag events → per-tab per-line diagnostic markers.
  useEffect(() => {
    if (!isGo) return
    const off = Events.On('lsp.diag', (ev: any) => {
      const d = (ev.data ?? {}) as LSP.LSPDiagEvent
      if (!d || d.projectId !== projectId || d.path !== tab.path) return
      viewRef.current?.dispatch({
        effects: setDiagEffect.of((d.diagnostics ?? []) as LSP.Diagnostic[]),
      })
    })
    return () => off()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isGo, projectId, tab.path])

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const content = tab.buffer?.content
    if (content !== undefined && content !== view.state.doc.toString()) {
      view.dispatch({
        changes: {
          from: 0,
          to: view.state.doc.length,
          insert: content,
        },
      })
    }
  }, [tab.buffer?.content])

  useEffect(() => {
    const view = viewRef.current
    if (!view || !tab.reveal) return
    // wait until content has actually landed so line numbers are valid
    if (view.state.doc.lines < 2 && tab.dirContent !== '') {
      if (tab.dirContent === undefined && view.state.doc.length === 0) return
    }
    const lineNo = Math.min(tab.reveal.line + 1, view.state.doc.lines)
    const line = view.state.doc.line(lineNo)
    const ch = Math.min(tab.reveal.character, line.length)
    view.dispatch({
      selection: { anchor: line.from + ch, head: line.from + ch },
      effects: EditorView.scrollIntoView(line.from + ch, { y: 'center' }),
    })
    view.focus()
    consumeReveal(projectId, tab.path)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.reveal, tab.dirContent])

  useEffect(() => {
    if (active) viewRef.current?.requestMeasure()
  }, [active])

  return (
    <div ref={wrapRef}
      className="absolute inset-0 flex flex-col"
      style={{ display: active ? 'flex' : 'none' }}
    >
      {tab.staleExternally && (
        <div className="flex items-center gap-3 px-3 py-1 text-xs bg-[#3b2f1e] border-b border-[#b58900]/40 text-[#e6c07b] shrink-0">
          <span>File changed on disk</span>
          <button
            className="no-drag px-2 py-0.5 rounded border border-[#e6c07b]/50 hover:bg-[#e6c07b]/15"
            onClick={() =>
              App.ReadFile(tab.path)
                .then((c) => reload(projectId, tab.path, c))
                .catch(() => {})
            }
          >
            Reload
          </button>
          <button
            className="no-drag px-2 py-0.5 rounded border border-[#e6c07b]/50 hover:bg-[#e6c07b]/15"
            onClick={() => keepMine(projectId, tab.path)}
          >
            Keep mine
          </button>
        </div>
      )}
      <div
        ref={hostRef}
        className="relative flex-1 min-h-0 overflow-hidden"
      />
      {inlineK && (
        <InlineKBar
          st={inlineK}
          onSubmit={runInlineK}
          onClose={() => setInlineK(null)}
        />
      )}
    </div>
  )
}

const MdPreviewTab: React.FC<{ tab: Tab; active: boolean }> = ({
  tab,
  active,
}) => (
  <div
    className="absolute inset-0 flex flex-col"
    style={{ display: active ? 'flex' : 'none' }}
  >
    <div className="flex-1 min-h-0 overflow-y-auto">
      <MdPreview content={tab.dirContent ?? ''} />
    </div>
  </div>
)

// ImageTab renders raster images (png/jpg/gif/webp/bmp/svg) as a centered
// preview instead of dumping bytes into the editor. dirContent holds the
// base64 payload fetched by TabsProvider.
const ImageTab: React.FC<{ tab: Tab; active: boolean }> = ({
  tab,
  active,
}) => (
  <div
    className="absolute inset-0 flex flex-col"
    style={{ display: active ? 'flex' : 'none' }}
  >
    <div className="flex-1 min-h-0 flex items-center justify-center overflow-auto p-6">
      {tab.dirContent ? (
        <img
          src={`data:image/${imageMime(tab.path)};base64,${tab.dirContent}`}
          alt={tab.title}
          className="max-w-full max-h-full object-contain"
          draggable={false}
        />
      ) : (
        <span className="text-dim text-xs">Loading image…</span>
      )}
    </div>
  </div>
)

const Editor: React.FC = () => {
  const { activeId } = useProjects()
  const { tabsByProject, saveError } = useTabs()
  const state = activeId ? tabsByProject[activeId] : undefined

  if (!activeId || !state || !state.active) {
    return (
      <div className="flex-1 flex items-center justify-center bg-editor min-h-0">
        <div className="w-3/4 h-1/2 border-2 border-dashed rounded-lg flex items-center justify-center text-dim">
          Editor surface {'\u2014'} open a file to start editing
        </div>
      </div>
    )
  }

  return (
    <div className="relative flex-1 min-h-0 bg-editor flex flex-col">
      {saveError && (
        <div className="px-3 py-1 text-xs bg-[#5a1d1d] border-b border-[#ff6b6b]/40 text-[#ff9999] shrink-0">
          {saveError}
        </div>
      )}
      <div className="relative flex-1 min-h-0">
        {state.open.map((t) =>
          t.kind === 'diff' ? (
            <DiffViewer
              key={t.path}
              projectId={activeId}
              tab={t}
              active={t.path === state.active}
            />
          ) : t.kind === 'md-preview' ? (
            <MdPreviewTab
              key={t.path}
              tab={t}
              active={t.path === state.active}
            />
          ) : t.kind === 'image' ? (
            <ImageTab key={t.path} tab={t} active={t.path === state.active} />
          ) : t.buffer ? (
            <TabEditor
              key={`${activeId}:${t.path}`}
              projectId={activeId}
              tab={t}
              active={t.path === state.active}
            />
          ) : (
            <div key={t.path} className="text-dim text-xs p-3" style={{ display: t.path === state.active ? undefined : 'none' }}>
              Loading file…
            </div>
          ),
        )}
      </div>
    </div>
  )
}

export default Editor
