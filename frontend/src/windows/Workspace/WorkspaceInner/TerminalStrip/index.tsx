import { useLayout } from "../../../../state/layout";
import { useProjects } from "../../../../state/projects";
import { useTerminal } from "../../../../state/terminal";
import TerminalPanel from "./TerminalPanel";




const TerminalStrip: React.FC = () => {
  const { activeId } = useProjects()
  const { ui } = useLayout()
  const { terminalsByProject, open, close, setActive } = useTerminal()
  const state = activeId ? terminalsByProject[activeId] : undefined

  return (
    <div
      className="collapsible shrink-0 bg-panel border-t border-panel flex flex-col min-h-0"
      style={{ height: ui.terminalHeight }}
    >
      <div className="flex items-center border-b border-panel text-xs shrink-0">
        {state?.open.map((t, i) => (
          <div key={t.termId} className="flex items-center">
            <button
              className={
                'px-3 py-2 border-l-2 ' +
                (state.active === t.termId
                  ? 'border-(--accent) text-primary'
                  : 'border-transparent text-dim hover:text-primary')
              }
              onClick={() => activeId && setActive(activeId, t.termId)}
            >
              {t.label ?? `sh ${i + 1}`}
            </button>
            <button
              className="mr-1 w-4 h-4 rounded flex items-center justify-center text-dim hover:text-primary hover:bg-[#373940]"
              title="Close terminal"
              aria-label={`Close ${t.label ?? `sh ${i + 1}`}`}
              onClick={() => close(t.termId)}
            >
              {'\u00D7'}
            </button>
          </div>
        ))}
        <button
          className="px-2 py-2 text-dim hover:text-primary"
          title="New terminal"
          aria-label="New terminal"
          onClick={() => open()}
        >
          {'\uFF0B'}
        </button>
      </div>
      <div className="flex-1 min-h-0 relative">
        {activeId && state?.open.length ? (
          state.open.map((t) => (
            <TerminalPanel
              key={t.termId}
              termId={t.termId}
              visible={state.active === t.termId}
            />
          ))
        ) : (
          <div className="flex h-full items-center justify-center text-dim text-xs">
            No terminal — click + to open
          </div>
        )}
      </div>
    </div>
  )
}

export default TerminalStrip