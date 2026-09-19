import Icon from "../../../components/Icon";
import { useGit } from "../../../state/git";
import { LayoutUI } from "../../../state/layout";
import { useProjects } from "../../../state/projects";
import { railBtn } from "../utils";



type dockTabType = 'search' | 'agent' | 'git' | 'settings'



interface Props {
  ui: LayoutUI
  dockTab: dockTabType
  onToggle: (key: 'sidebar' | 'rightDock' | 'terminal') => void
}



const ActivityRail: React.FC<Props> = ({ ui, dockTab, onToggle }) => {
  const { activeId } = useProjects()
  const { status } = useGit()
  const st = activeId ? status[activeId] : undefined
  const changed = st
    ? (st.staged?.length ?? 0) +
      (st.unstaged?.length ?? 0) +
      (st.untracked?.length ?? 0)
    : 0

  const openDockTab = (tab: dockTabType) => {
    // Re-clicking the rail icon of the tab the dock is already showing
    // closes the dock instead of keeping it pinned open.
    if (ui.rightDock && dockTab === tab) {
      onToggle('rightDock')
      return
    }
    window.dispatchEvent(
      new CustomEvent('codesaber:set-dock-tab', { detail: { tab } }),
    )
    if (!ui.rightDock) onToggle('rightDock')
  }

  const disabled = (label: string) => (
    <button
      className="no-drag w-7 h-7 rounded-md flex items-center justify-center text-dim/60 cursor-default"
      title={label}
      aria-label={label}
      disabled
    >
      <Icon name="grid" />
    </button>
  )

  return (
    <div className="flex flex-col items-center gap-1.5 py-2 bg-panel w-11 shrink-0 border-r border-panel">
      <button
        className={railBtn(ui.sidebar)}
        title="Files ⌘B"
        aria-label="Toggle files sidebar"
        onClick={() => onToggle('sidebar')}
      >
        <Icon name="file" />
      </button>
      <button
        className={railBtn(ui.rightDock && dockTab === 'search')}
        title="Search ⌘⇧F"
        aria-label="Open search dock"
        onClick={() => openDockTab('search')}
      >
        <Icon name="search" />
      </button>
      <button
        className={railBtn(ui.rightDock && dockTab === 'git')}
        title="Source Control ⌘D"
        aria-label="Toggle git dock"
        onClick={() => openDockTab('git')}
      >
        <Icon name="branch" />
        {changed > 0 && (
          <span className="absolute -top-0.5 -right-1 rounded-full bg-[#3b5bfd] text-[9px] text-white px-1 h-3.5 min-w-3.5 flex items-center justify-center font-medium">
            {changed > 99 ? '99+' : changed}
          </span>
        )}
      </button>
      <button
        className={railBtn(ui.terminal)}
        title="Terminal ⌘J"
        aria-label="Toggle terminal"
        onClick={() => onToggle('terminal')}
      >
        <Icon name="terminal" />
      </button>
      <div className="mt-auto flex flex-col items-center gap-1.5">
        <button
          className={railBtn(ui.rightDock && dockTab === 'settings')}
          title="Settings"
          aria-label="Open settings dock"
          onClick={() => openDockTab('settings')}
        >
          <Icon name="gear" />
        </button>
        {disabled('Extensions — coming soon')}
      </div>
    </div>
  )
}


export default ActivityRail