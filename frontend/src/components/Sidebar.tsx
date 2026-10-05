import React, { useState } from 'react'
import FileTree from './FileTree'
import { shortcut } from '../lib/platform'
import { useProjects } from '../state/projects'
import { useLayout } from '../state/layout'

// Collapsible project sections: collapsed project ids are persisted per
// project id in localStorage under 'codesaber.projects.expand' so they survive
// restarts. Default is expanded; chevron rotates per state.

const readCollapsedMap = (): Record<string, boolean> => {
  try {
    return JSON.parse(localStorage.getItem('codesaber.projects.expand') ?? '{}')
  } catch {
    return {}
  }
}

const Chevron: React.FC<{ expanded: boolean }> = ({ expanded }) => (
  <svg
    width="10"
    height="10"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2.4"
    strokeLinecap="round"
    strokeLinejoin="round"
    className={
      'shrink-0 transition-transform duration-150 ' +
      (expanded ? 'rotate-90' : 'rotate-0')
    }
  >
    <path d="M9 5l7 7-7 7" />
  </svg>
)

const Sidebar: React.FC = () => {
  const { projects, activeId, open, remove, setActive } = useProjects()
  const { ui, toggle } = useLayout()
  const [collapsedMap, setCollapsedMap] = useState<Record<string, boolean>>(
    readCollapsedMap,
  )
  const toggleCollapsed = (id: string) =>
    setCollapsedMap((prev) => {
      const next = { ...prev, [id]: !prev[id] }
      try {
        localStorage.setItem(
          'codesaber.projects.expand',
          JSON.stringify(next),
        )
      } catch {
        // quota/private-mode errors: collapse state stays session-only
      }
      return next
    })

  return (
    <div
      className={
        'collapsible relative shrink-0 border-r border-panel bg-panel h-full flex flex-col text-xs ' +
        (ui.sidebar ? '' : 'collapsed')
      }
      style={{ width: ui.sidebar ? ui.sidebarWidth : undefined }}
    >
      <button
        onClick={() => toggle('sidebar')}
        className="no-drag absolute top-1.5 right-1 z-10 w-5 h-5 rounded flex items-center justify-center text-dim hover:text-primary hover:bg-[#373940]"
        title={`Collapse sidebar (${shortcut('B')})`}
        aria-label="Collapse sidebar"
      >
        {'\u2039'}
      </button>
      <button
        onClick={() => void open()}
        className="no-drag mx-2 mt-2 mb-1 px-2 py-1.5 rounded bg-[#2b4d75] hover:bg-[#35597e] text-left text-[11px]"
      >
        + Add Project
      </button>
      <div className="flex-1 overflow-y-auto">
        {projects.map((p) => (
          <div key={p.id} className="mb-1 border-b border-panel pb-1">
            <div
              className={
                'flex items-center gap-1 px-2 py-1.5 cursor-default ' +
                (p.id === activeId ? 'bg-[#373940]' : 'hover:bg-[#2e3037]')
              }
              onClick={() => setActive(p.id)}
            >
              <button
                className="no-drag flex items-center justify-center w-3 h-3 shrink-0 text-dim hover:text-primary"
                title={collapsedMap[p.id] ? 'Expand' : 'Collapse'}
                aria-label={`Toggle file tree for ${p.name}`}
                aria-expanded={!collapsedMap[p.id]}
                onClick={(e) => {
                  e.stopPropagation()
                  toggleCollapsed(p.id)
                }}
              >
                <Chevron expanded={!collapsedMap[p.id]} />
              </button>
              <span
                className={
                  'w-1.5 h-1.5 rounded-full shrink-0 ' +
                  (p.engineOk ? 'bg-[#4a9e6b]' : 'bg-[#c75454]')
                }
                title={p.engineOk ? 'watcher running' : 'watcher unavailable'}
              />
              <span className="flex-1 truncate text-primary" title={p.root}>
                {p.name}
              </span>
              {p.branch && (
                <span className="px-1 rounded bg-[#373940] text-dim text-[9px]">
                  {p.branch}
                </span>
              )}
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  window.dispatchEvent(
                    new CustomEvent('codesaber:filetree-new', {
                      detail: { projectId: p.id },
                    }),
                  )
                }}
                className="no-drag px-1 text-dim hover:text-primary"
                title="New file or folder"
                aria-label={`New file or folder in ${p.name}`}
              >
                +
              </button>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  void remove(p.id)
                }}
                className="no-drag px-1 text-dim hover:text-primary"
                title="Remove project"
              >
                &times;
              </button>
            </div>
            {!collapsedMap[p.id] && (
              <FileTree root={p.root} projectId={p.id} />
            )}
          </div>
        ))}
        {projects.length === 0 && (
          <div className="px-3 py-2 text-dim text-[10px]">No projects open</div>
        )}
      </div>
      <div className="px-3 py-2 border-t border-panel text-dim text-[10px]">
        Phase 1 {'\u00b7'} placeholder
      </div>
    </div>
  )
}

export default Sidebar
