// Vim mode for the editor via @replit/codemirror-vim; `status: true` renders
// the --NORMAL--/--INSERT-- panel at the bottom (themed below).
import { EditorView } from '@codemirror/view'
import type { Extension } from '@codemirror/state'
import { CodeMirror, vim } from '@replit/codemirror-vim'

const vimPanelTheme = EditorView.theme(
  {
    '.cm-vim-panel': {
      display: 'flex',
      alignItems: 'center',
      gap: '8px',
      padding: '2px 10px',
      fontFamily: '"SF Mono", Menlo, Monaco, monospace',
      fontSize: '11px',
      backgroundColor: 'var(--bg-panel)',
      color: 'var(--text-dim)',
      borderTop: '1px solid var(--bg-border)',
    },
    '.cm-vim-panel input': {
      flex: '1 1 auto',
      minWidth: '0',
      fontFamily: 'inherit',
      fontSize: 'inherit',
      backgroundColor: 'transparent',
      color: 'var(--text-primary)',
      border: 'none',
      outline: 'none',
    },
  },
  { dark: true },
)

// The Ex dialog's `:w`/`:wq`/`:x` runs the library's `write` command, which
// looks up CodeMirror.commands.save (undefined by default, so it would no-op).
// Editors register their save handler per view and it dispatches from there.
const vimSaveHandlers = new WeakMap<EditorView, () => void>()

export const setVimSaveHandler = (view: EditorView, save: () => void) => {
  vimSaveHandlers.set(view, save)
}

CodeMirror.commands.save = (cm: { cm6: EditorView }) => {
  vimSaveHandlers.get(cm.cm6)?.()
}

export const vimExtension = (): Extension => [vim({ status: true }), vimPanelTheme]
