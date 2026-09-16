import React from 'react'
import { shortcut } from '../lib/platform'
import { useProjects } from '../state/projects'
import { useTabs } from '../state/tabs'
import { useGit } from '../state/git'
import { langChip } from '../lib/filetype'

const iconProps = {
  width: 15,
  height: 15,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

const FolderIcon: React.FC = () => (
  <svg {...iconProps}>
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2.5h8a2 2 0 0 1 2 2V17a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" />
  </svg>
)

const BranchIcon: React.FC = () => (
  <svg {...iconProps} width={11} height={11}>
    <circle cx="6" cy="6" r="2.4" />
    <circle cx="6" cy="18" r="2.4" />
    <circle cx="18" cy="8" r="2.4" />
    <path d="M6 8.4v7.2" />
    <path d="M18 10.4c0 3.4-2.8 4.4-6.2 4.6" />
  </svg>
)

const SearchIcon: React.FC = () => (
  <svg {...iconProps}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="M20 20l-3.8-3.8" />
  </svg>
)

const Titlebar: React.FC = () => {
  const { projects, activeId } = useProjects()
  const { tabsByProject } = useTabs()
  const { status } = useGit()

  const active = projects.find((p) => p.id === activeId) ?? null
  const branch = (activeId ? status[activeId]?.branch : undefined) ?? active?.branch
  const tabsState = activeId ? tabsByProject[activeId] : undefined
  const activeTab = tabsState?.active
    ? tabsState.open.find((t) => t.path === tabsState.active)
    : undefined

  const openQuickOpen = () => window.dispatchEvent(new Event('codesaber:quickopen'))

  return (
    <div
      className="drag-region h-10 shrink-0 flex items-center bg-panel text-xs border-b border-panel"
      style={{ paddingLeft: 72 }}
    >
      {/* Breadcrumb: project › branch chip › active file */}
      <div className="no-drag flex items-center min-w-0 shrink max-w-[min(45vw,420px)] overflow-hidden">
        {active ? (
          <>
            <span className="flex items-center gap-1.5 text-primary" title={active.root}>
              <FolderIcon />
              <span className="truncate">{active.name}</span>
            </span>
            {branch && (
              <>
                <span className="text-dim px-1.5 opacity-60">›</span>
                <span
                  className="flex items-center gap-1 px-1.5 h-[18px] rounded-full bg-[#1e1f22] text-[11px] text-dim shrink-0"
                  title={`branch ${branch}`}
                >
                  <BranchIcon />
                  <span className="max-w-[140px] truncate">{branch}</span>
                </span>
              </>
            )}
            {activeTab && (
              <>
                <span className="text-dim px-1.5 opacity-60">›</span>
                <span className="flex items-center gap-1.5 min-w-0" title={activeTab.path}>
                  {langChip(activeTab.path)}
                  <span className="truncate text-primary">{activeTab.title}</span>
                </span>
              </>
            )}
          </>
        ) : (
          <span className="flex items-center gap-1.5 text-dim" title="active project">
            <FolderIcon />
            codesaber
          </span>
        )}
      </div>

      {/* Search capsule — visually centered, hidden tail on narrow windows */}
      <div className="flex-1 flex justify-center px-3 min-w-0">
        <button
          className="no-drag h-[24px] w-full max-w-[520px] flex items-center gap-2 px-2.5 rounded-full bg-[#1e1f22] border border-[var(--bg-border)] text-dim hover:border-[#4a4d54] hover:text-[#9a9da3] transition-colors"
          title={`Search files, symbols & actions (${shortcut('P')})`}
          aria-label="Open quick open"
          onClick={openQuickOpen}
        >
          <SearchIcon />
          <span className="truncate text-[11px]">Search files, symbols &amp; actions</span>
          <span className="ml-auto shrink-0 px-1.5 h-[16px] rounded bg-white/8 text-[10px] leading-[16px] text-[#9a9da3]">
            {shortcut('P')}
          </span>
        </button>
      </div>
    </div>
  )
}

export default Titlebar
