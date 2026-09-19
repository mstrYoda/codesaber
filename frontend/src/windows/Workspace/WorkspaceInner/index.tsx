import { useEffect, useRef, useState } from "react";
import { SHORTCUTS, useKeyboardShortcut } from "../../../state/keybinding";
import { SIZE_LIMITS, useLayout } from "../../../state/layout";
import { useProjects } from "../../../state/projects";
import { useTabs } from "../../../state/tabs";
import { useGit } from "../../../state/git";
import { requestCloseActive } from "../utils";
import { Events } from "@wailsio/runtime";
import Titlebar from "../../../components/Titlebar";
import ActivityRail from "./ActivityRail";
import Sidebar from "../../../components/Sidebar";
import ResizeHandle from "../../../components/ResizeHandle";
import EditorTabs from "../../../components/EditorTabs";
import Breadcrumbs from "../../../components/Breadcrumbs";
import Editor from "../../../components/Editor";
import Icon from "../../../components/Icon";
import SearchPanel from "../../../components/SearchPanel";
import AgentPanel from "../../../components/AgentPanel";
import SettingsPanel from "../../../components/SettingsPanel";
import GitPanel from "../../../components/GitPanel";
import StatusBar from "../../../components/StatusBar";
import QuickOpen from "../../../components/QuickOpen";
import CommandPalette from "../../../components/CommandPalette";
import PerfHUD from "../../../components/PerfHUD";
import TerminalStrip from "./TerminalStrip";

const WorkspaceInner: React.FC = () => {
  const { ui, toggle, setSize } = useLayout()
  const [dockTab, setDockTab] = useState<
    'search' | 'agent' | 'git' | 'settings'
  >('git')
  const { activeId } = useProjects()
  const { tabsByProject, close } = useTabs()
  const { status } = useGit()
  const st = activeId ? status[activeId] : undefined
  const changed = st
    ? (st.staged?.length ?? 0) +
      (st.unstaged?.length ?? 0) +
      (st.untracked?.length ?? 0)
    : 0     

  // Rail → dock tab coordination: the rail can force the dock onto a tab
  // (search / git badge click) without lifting tab state into the layout
  // provider.
  useEffect(() => {
    const onTab = (e: Event) => {
      const tab = (e as CustomEvent<{ tab?: string }>).detail?.tab
      if (
        tab === 'search' ||
        tab === 'agent' ||
        tab === 'git' ||
        tab === 'settings'
      )
        setDockTab(tab)
    }
    window.addEventListener('codesaber:set-dock-tab', onTab)
    return () => window.removeEventListener('codesaber:set-dock-tab', onTab)
  }, [])

  useKeyboardShortcut('toggleSidebar', () => toggle('sidebar'))
  useKeyboardShortcut('toggleTerminal', () => toggle('terminal'))
  useKeyboardShortcut('toggleRightDock', () => {
    // Mirrors ActivityRail's Source Control button: re-toggling while the
    // dock is already pinned to Git closes it instead of leaving it open.
    if (ui.rightDock && dockTab === 'git') {
      toggle('rightDock')
      return
    }
    setDockTab('git')
    if (!ui.rightDock) toggle('rightDock')
  })

  // ⌘W → close active tab, not window. Two cooperating paths:
  //  1. Webview keydown (below, capture phase): preventDefault + immediately
  //     close AND stamp window.__codesaberWAt.
  //  2. Native File → Close Tab menu accelerator (main.go) emits
  //     'codesaber:close-tab'. If the menu interceptor wins (its keyEquivalent is
  //     matched before the webview on some macOS paths), marker stays stale
  //     and this event closes the tab. If the webview already closed, the
  //     event is skipped via the marker.
  // Either way requestCloseActive's 200ms debounce guarantees exactly one
  // close per key press. On the webview side, preventDefault is harmless
  // ( WKWebView does not reserve ⌘W, but no default close action exists in the
  // page ). A close request with no open tab is a no-op.
  const closeActiveRef = useRef(() => {})
  closeActiveRef.current = () => {
    const st = activeId ? tabsByProject[activeId] : undefined
    if (activeId && st?.active) close(activeId, st.active)
  }
  useEffect(() => {
    const closeActive = () => closeActiveRef.current()
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey && !e.ctrlKey) return
      if (e.shiftKey || e.altKey) return
      if (e.key.toLowerCase() !== SHORTCUTS.closeTab.key) return
      e.preventDefault()
      e.stopPropagation()
      window.__codesaberWAt = Date.now()
      requestCloseActive(closeActive)
    }
    const off = Events.On('codesaber:close-tab', () => {
      // marker fresh → webview path already (or is about to) close
      if (Date.now() - (window.__codesaberWAt ?? 0) < 100) return
      requestCloseActive(closeActive)
    })
    // File → AI: Edit Selection (main.go) relays into the editor surface via
    // a window event; TabEditor decides whether a selection exists.
    const offAi = Events.On('codesaber:ai-edit', () => {
      window.dispatchEvent(new Event('codesaber:ai-edit'))
    })
    window.addEventListener('keydown', onKey, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      offAi()
      off()
    }
  }, [])


  return (
    <div className="flex flex-col h-full bg-editor text-primary">
      <Titlebar />
      <div className="flex flex-1 min-h-0">
        {/* Activity rail */}
        <ActivityRail ui={ui} dockTab={dockTab} onToggle={toggle} />
        {/* Left sidebar */}
        <Sidebar />
        {/* Resize handle between sidebar and editor area */}
        {ui.sidebar && (
          <ResizeHandle
            axis="x"
            onResize={(px) => setSize('sidebarWidth', px)}
            onReset={() => setSize('sidebarWidth', SIZE_LIMITS.sidebarWidth.def)}
            getCurrent={() => ui.sidebarWidth}
          />
        )}
        {/* Center editor area */}
        <div className="flex flex-col flex-1 min-w-0 bg-editor">
          <EditorTabs />
          <Breadcrumbs />
          <Editor />
        </div>
        {/* Resize handle between editor area and right dock */}
        {ui.rightDock && (
          <ResizeHandle
            axis="x"
            onResize={(px) => setSize('rightDockWidth', px)}
            onReset={() =>
              setSize('rightDockWidth', SIZE_LIMITS.rightDockWidth.def)
            }
            getCurrent={() => ui.rightDockWidth}
            flip
          />
        )}
        {/* Right dock */}
        <div
          className={
            'collapsible shrink-0 bg-panel border-l border-panel h-full flex flex-col ' +
            (ui.rightDock ? '' : 'collapsed')
          }
          style={{ width: ui.rightDock ? ui.rightDockWidth : undefined }}
        >
          <div className="flex items-center border-b border-panel text-xs">
            <button
              className={
                'relative flex items-center gap-1.5 px-3 py-2 ' +
                (dockTab === 'search'
                  ? 'text-primary after:absolute after:left-2 after:right-2 after:bottom-0 after:h-0.5 after:bg-(--accent) after:rounded-t'
                  : 'text-dim hover:text-primary')
              }
              onClick={() => setDockTab('search')}
            >
              <Icon name="search" />
              Search
            </button>
            <button
              className={
                'relative flex items-center gap-1.5 px-3 py-2 ' +
                (dockTab === 'agent'
                  ? 'text-primary after:absolute after:left-2 after:right-2 after:bottom-0 after:h-0.5 after:bg-(--accent) after:rounded-t'
                  : 'text-dim hover:text-primary')
              }
              onClick={() => setDockTab('agent')}
            >
              <Icon name="bot" />
              Agent
            </button>
            <button
              className={
                'relative flex items-center gap-1.5 px-3 py-2 ' +
                (dockTab === 'git'
                  ? 'text-primary after:absolute after:left-2 after:right-2 after:bottom-0 after:h-0.5 after:bg-(--accent) after:rounded-t'
                  : 'text-dim hover:text-primary')
              }
              onClick={() => setDockTab('git')}
            >
              <Icon name="branch" />
              Git
              {changed > 0 && (
                <span className="rounded-full bg-[#3b5bfd] text-[9px] text-white px-1 h-3.5 min-w-3.5 flex items-center justify-center font-medium">
                  {changed > 99 ? '99+' : changed}
                </span>
              )}
            </button>
            <button
              className={
                'relative flex items-center gap-1.5 px-3 py-2 ' +
                (dockTab === 'settings'
                  ? 'text-primary after:absolute after:left-2 after:right-2 after:bottom-0 after:h-0.5 after:bg-(--accent) after:rounded-t'
                  : 'text-dim hover:text-primary')
              }
              onClick={() => setDockTab('settings')}
            >
              <Icon name="gear" />
              Settings
            </button>
            <button
              onClick={() => toggle('rightDock')}
              className="no-drag ml-auto mr-1 w-5 h-5 rounded flex items-center justify-center text-dim hover:text-primary hover:bg-[#373940]"
              title="Collapse dock (⌘D)"
              aria-label="Collapse right dock"
            >
              {'\u00bb'}
            </button>
          </div>
          <div className="flex-1 min-h-0 flex flex-col">
            {dockTab === 'search' ? (
              <SearchPanel />
            ) : dockTab === 'agent' ? (
              <AgentPanel />
            ) : dockTab === 'settings' ? (
              <SettingsPanel />
            ) : (
              <GitPanel />
            )}
          </div>
        </div>
      </div>
      <StatusBar />
      <CommandPalette />
      <QuickOpen />
      <PerfHUD />
      {/* Bottom strip: terminal */}
      {ui.terminal && (
        <>
          <ResizeHandle
            axis="y"
            flip
            onResize={(px) => setSize('terminalHeight', px)}
            onReset={() =>
              setSize('terminalHeight', SIZE_LIMITS.terminalHeight.def)
            }
            getCurrent={() => ui.terminalHeight}
          />
          <TerminalStrip />
        </>
      )}
    </div>
  )
}


export default WorkspaceInner