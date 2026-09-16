import React from 'react'
import { useProjects } from '../state/projects'
import { useTabs, parseDiffTabPath } from '../state/tabs'

// Breadcrumbs-under-tabs: slim strip showing the active tab's path
// (project name › path segments) with dim, non-interactive text.
const Breadcrumbs: React.FC = () => {
  const { projects, activeId } = useProjects()
  const { tabsByProject } = useTabs()
  const state = activeId ? tabsByProject[activeId] : undefined
  const key = state?.active ?? null
  const tab = state?.open.find((t) => t.path === key) ?? null
  if (!key || !tab) return null

  // diff tab keys are Δ:s|u:<real path> — reveal the underlying file
  const realPath = tab.kind === 'diff' ? parseDiffTabPath(key)?.path : tab.path
  if (!realPath) return null

  const project = projects.find((p) => p.id === activeId)
  let rel = realPath
  const root = project?.root
  if (root && rel.startsWith(root)) rel = rel.slice(root.length)
  rel = rel.replace(/^[\\/]/, '')
  const segs = rel.split(/[\\/]/).filter(Boolean)
  if (!segs.length) return null

  const parts = project ? [project.name, ...segs] : [...segs]

  return (
    <div className="flex items-center gap-1 h-7 px-3 bg-editor text-dim text-[11px] shrink-0 overflow-x-auto whitespace-nowrap">
      {parts.map((seg, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="opacity-50">{'\u203a'}</span>}
          <span className={i === parts.length - 1 ? 'opacity-70' : ''}>
            {seg}
          </span>
        </React.Fragment>
      ))}
    </div>
  )
}

export default Breadcrumbs
