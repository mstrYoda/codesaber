import React, { useEffect, useMemo, useRef, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import type { SessionMeta } from '../../bindings/codesaber/backend/agentstore/models'
import { useProjects } from '../state/projects'
import {
  useAgent,
  type TimelineItem,
  type MessageItem,
  type ToolItem,
  type PermissionItem,
} from '../state/agent'
import MiniDiff, { diffCounts } from './MiniDiff'
import ConfirmDialog from './ConfirmDialog'
import ProviderSetup from './ProviderSetup'
import ModelPicker from './ModelPicker'

// ---- helpers -------------------------------------------------------------
const statusChip = (status: string) => {
  if (status === 'in_progress' || status === 'pending')
    return { dot: 'bg-[#e6c07b] animate-pulse', label: 'running', cls: 'text-[#e6c07b]' }
  if (status === 'completed')
    return { dot: 'bg-[#7dcf9e]', label: 'done', cls: 'text-[#7dcf9e]' }
  if (status === 'failed')
    return { dot: 'bg-[#e5735f]', label: 'failed', cls: 'text-[#e5735f]' }
  return { dot: 'bg-gray-500', label: status, cls: 'text-dim' }
}

const toolGlyph = (kind: string) =>
  kind === 'read' ? '📄' : kind === 'edit' ? '✏️' : kind === 'search' ? '⌕' : '🔧'

const timeAgo = (iso: string) => {
  const t = Date.parse(iso)
  if (isNaN(t)) return iso
  const s = Math.max(1, Math.floor((Date.now() - t) / 1000))
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

// ---- timeline item renderers --------------------------------------------
const UserBubble: React.FC<{ m: MessageItem }> = ({ m }) => (
  <div className="flex justify-end px-3 py-1">
    <div className="max-w-[85%] rounded-lg border border-[var(--accent)]/35 bg-[var(--accent)]/15 text-primary text-xs px-3 py-1.5 whitespace-pre-wrap break-words">
      {m.text}
    </div>
  </div>
)

const AgentText: React.FC<{ m: MessageItem }> = ({ m }) => (
  <div
    className={
      'px-3 py-1 text-xs whitespace-pre-wrap break-words ' +
      (m.kind === 'error' ? 'text-[#e5735f]' : m.role === 'system' ? 'text-dim text-center text-[10px]' : 'text-[#bcbec4]')
    }
  >
    {m.role === 'system' ? <span className="px-2 py-0.5 rounded bg-[#1e1f22]">{m.text}</span> : m.text}
  </div>
)

const ToolCard: React.FC<{ t: ToolItem }> = ({ t }) => {
  const [open, setOpen] = useState(false)
  const chip = statusChip(t.status)
  return (
    <div className="mx-3 my-0.5 rounded-lg border border-[#333639] bg-[#242629] overflow-hidden">
      <button
        className="w-full flex items-center gap-2 px-2.5 py-1.5 text-[11px] text-left"
        onClick={() => setOpen((o) => !o)}
        title={open ? 'Collapse' : 'Expand'}
      >
        <span className={'text-dim text-[10px] transition-transform ' + (open ? 'rotate-90' : '')}>›</span>
        <span>{toolGlyph(t.kind)}</span>
        <span className="truncate flex-1 min-w-0 text-[#bcbec4]">
          {t.title || t.toolCallId}
        </span>
        <span className={'shrink-0 flex items-center gap-1.5 ' + chip.cls}>
          <span className={'w-1.5 h-1.5 rounded-full ' + chip.dot} />
          <span className="text-[10px]">{chip.label}</span>
        </span>
      </button>
      {open && t.content && (
        <pre className="border-t border-[#26282b] px-2.5 py-1.5 text-[10px] text-dim whitespace-pre-wrap break-words max-h-40 overflow-y-auto">
          {t.content}
        </pre>
      )}
    </div>
  )
}

const PermissionCard: React.FC<{ p: PermissionItem; onRespond: (optionId: string, cancel: boolean) => void }> = ({ p, onRespond }) => {
  if (p.purpose === 'fs-write') {
    const oldText = p.oldText ?? ''
    const newText = p.newText ?? ''
    const counts = diffCounts(oldText, newText)
    return (
      <div className="mx-3 my-1 rounded-lg border border-[#e6c07b]/40 bg-[#1e1f22] p-2">
        <div className="flex items-center gap-2 mb-1">
          <span className="text-[11px] text-[#e6c07b] font-semibold">
            {p.isNew ? 'Create file' : 'Edit file'}
          </span>
          <span className="text-[11px] text-dim truncate min-w-0" title={p.path}>{p.path}</span>
          <span className="ml-auto shrink-0 text-[10px] font-mono">
            <span className="text-[#7dcf9e]">+{counts.adds}</span>{' '}
            <span className="text-[#e5735f]">−{counts.dels}</span>
          </span>
        </div>
        <MiniDiff oldText={oldText} newText={newText} />
        {p.truncated && (
          <div className="mt-1 text-[10px] text-[#e6c07b]/80">preview truncated — open full diff after applying</div>
        )}
        <div className="flex gap-1.5 mt-1.5">
          <button className="no-drag px-3 py-1 rounded bg-[#2a2c31] text-[#e5735f] hover:bg-[#373940]" onClick={() => onRespond('', true)}>Reject</button>
          <button className="no-drag ml-auto px-3 py-1 rounded bg-[var(--accent)] text-[#0b0c10] font-medium hover:opacity-90" onClick={() => onRespond('allow', false)}>Accept</button>
        </div>
      </div>
    )
  }
  return (
    <div className="mx-3 my-1 rounded-lg border border-[#e6c07b]/40 bg-[#1e1f22] p-2">
      <div className="text-[11px] text-[#e6c07b] mb-1">Permission requested</div>
      {p.options.length === 0 ? (
        <div className="text-dim">(no options offered)</div>
      ) : (
        <div className="flex flex-col gap-1">
          {p.options.map((o) => (
            <button
              key={o.optionId ?? o.name}
              className="text-left px-2 py-1 rounded bg-[#2a2c31] hover:bg-[#373940]"
              onClick={() => onRespond(o.optionId ?? '', false)}
            >
              <span className="text-primary">{o.name ?? o.optionId}</span>
              {o.description && <span className="text-dim"> — {o.description}</span>}
            </button>
          ))}
          <button className="text-left px-2 py-1 rounded text-[#e5735f] hover:bg-[#2a2c31]" onClick={() => onRespond('', true)}>
            Reject
          </button>
        </div>
      )}
    </div>
  )
}

const TimelineItemView: React.FC<{ item: TimelineItem; onRespond: (requestId: string, optionId: string, cancel: boolean) => void }> = ({ item, onRespond }) => {
  if (item.type === 'message') return item.role === 'user' ? <UserBubble m={item} /> : <AgentText m={item} />
  if (item.type === 'tool') return <ToolCard t={item} />
  return (
    <PermissionCard
      p={item}
      onRespond={(optionId, cancel) => onRespond(item.requestId, optionId, cancel)}
    />
  )
}

// ---- history tab ---------------------------------------------------------
const SessionRow: React.FC<{
  s: SessionMeta
  active: boolean
  onOpen: () => void
  onRename: (title: string) => void
  onDelete: () => void
}> = ({ s, active, onOpen, onRename, onDelete }) => {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(s.title)
  return (
    <div className="mb-2 px-1 py-1 rounded hover:bg-white/4">
      <div className="flex items-center gap-2 cursor-pointer" onClick={() => setOpen((o) => !o)}>
        <span className={'w-1.5 h-1.5 rounded-full shrink-0 ' + (active ? 'bg-[#7dcf9e]' : 'bg-[#5b3fd4]')} />
        <span className="text-white truncate text-xs flex-1 min-w-0">{s.title || '(untitled)'}</span>
        <span className="text-[10px] text-dim shrink-0">{timeAgo(s.updated)}</span>
      </div>
      <div className="pl-3.5 text-[11px] text-dim">
        {s.messageCount} messages
      </div>
      {open && (
        <div className="mt-1 ml-3.5 flex flex-col gap-1">
          {editing ? (
            <div className="flex gap-1">
              <input
                autoFocus
                className="flex-1 bg-[#1e1f22] border border-[#333639] rounded px-2 py-1 text-xs text-primary outline-none focus:ring-1 focus:ring-[var(--accent)]"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    onRename(title.trim() || s.title)
                    setEditing(false)
                  }
                  if (e.key === 'Escape') setEditing(false)
                }}
              />
              <button className="px-2 rounded bg-[var(--accent)] text-[#0b0c10] text-xs" onClick={() => { onRename(title.trim() || s.title); setEditing(false) }}>Save</button>
            </div>
          ) : (
            <div className="flex gap-2 text-[10px] uppercase tracking-wide">
              <button className="text-[var(--accent)] hover:underline" onClick={onOpen}>Open</button>
              <button className="text-dim hover:text-primary" onClick={() => { setTitle(s.title); setEditing(true) }}>Rename</button>
              <button className="text-[#e5735f] hover:underline" onClick={onDelete}>Delete</button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ---- main panel ----------------------------------------------------------
const AgentPanel: React.FC = () => {
  const { activeId } = useProjects()
  const {
    state, sessions, activeSession,
    send, start, stop, newSession, clearTranscript, respondPermission,
    listSessions, openSession, deleteSession, renameSession,
  } = useAgent()
  const [draft, setDraft] = useState('')
  const [modelBusy, setModelBusy] = useState(false)
  const [tab, setTab] = useState<'chat' | 'history'>('chat')
  const [confirmDelete, setConfirmDelete] = useState<SessionMeta | null>(null)
  const [histErr, setHistErr] = useState<string | null>(null)
  const listRef = useRef<HTMLDivElement | null>(null)
  const taRef = useRef<HTMLTextAreaElement | null>(null)
  const histRef = useRef<Record<string, string[]>>({})
  const histIdxRef = useRef<Record<string, number>>({})
  const draftRef = useRef(draft)
  draftRef.current = draft
  const stickRef = useRef(true)
  const [showJump, setShowJump] = useState(false)

  const st = activeId ? state[activeId] : undefined
  const running = !!st?.harness && st.status !== 'harness-down' && st.status !== 'no-harness'
  const thinking = st?.status === 'thinking'
  const timeline = st?.timeline ?? []
  const projSessions = (activeId && sessions[activeId]) || []
  const activeSID = (activeId && activeSession[activeId]) || null

  // prompt history (unchanged behavior)
  useEffect(() => {
    if (!activeId || histRef.current[activeId]) return
    App.ACPLoadTranscript(activeId)
      .then((entries) => {
        if (histRef.current[activeId]) return
        const list = (entries ?? [])
          .filter((e) => e.role === 'user' && e.text.trim())
          .slice(-30)
          .map((e) => e.text)
        histRef.current[activeId] = list
        histIdxRef.current[activeId] = list.length
      })
      .catch(() => {})
  }, [activeId])

  // History tab refreshes its session list on open; errors clear on tab switch
  useEffect(() => {
    if (activeId) setHistErr(null)
  }, [tab, activeId])
  useEffect(() => {
    if (tab === 'history' && activeId) void listSessions(activeId)
  }, [tab, activeId, listSessions])

  // ↑/↓ walk the per-project prompt history; only engages when composer empty
  // or already showing a recalled entry, so in-flight typing is never clobbered
  const recall = (dir: -1 | 1): boolean => {
    if (!activeId) return false
    const list = histRef.current[activeId]
    if (!list || !list.length) return false
    let idx = histIdxRef.current[activeId] ?? list.length
    if (dir === -1) {
      const cur = idx < list.length ? list[idx] : ''
      if (draftRef.current.trim() && draftRef.current !== cur) return false
      if (idx === list.length) idx = list.length - 1
      else if (idx > 0) idx -= 1
      else return false
    } else {
      if (idx >= list.length) return false
      idx += 1
    }
    histIdxRef.current[activeId] = idx
    setDraft(idx < list.length ? list[idx] : '')
    requestAnimationFrame(grow)
    return true
  }

  // stick-to-bottom unless the user scrolled up
  useEffect(() => {
    const el = listRef.current
    if (el && stickRef.current)     el.scrollTop = el.scrollHeight
  }, [timeline])

  const onScroll = () => {
    const el = listRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    stickRef.current = atBottom
    setShowJump(!atBottom)
  }

  const jumpToLatest = () => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
    stickRef.current = true
    setShowJump(false)
  }

  const grow = () => {
    const ta = taRef.current
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = Math.min(ta.scrollHeight, 140) + 'px'
  }

  const doSend = () => {
    const text = draft.trim()
    if (!text || !activeId || thinking || modelBusy || !running) return
    const hist = histRef.current[activeId] ?? (histRef.current[activeId] = [])
    if (hist[hist.length - 1] !== text) hist.push(text)
    histIdxRef.current[activeId] = hist.length
    setDraft('')
    requestAnimationFrame(grow)
    stickRef.current = true
    void send(activeId, text)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      doSend()
      return
    }
    if (e.key === 'ArrowUp') {
      if (recall(-1)) e.preventDefault()
      return
    }
    if (e.key === 'ArrowDown') {
      if (recall(1)) e.preventDefault()
    }
  }

  const counts = useMemo(() => {
    const tools = timeline.filter((i) => i.type === 'tool').length
    const msgs = timeline.filter((i) => i.type === 'message').length
    return { tools, msgs }
  }, [timeline])

  if (!activeId) {
    return (
      <div className="flex flex-col h-full items-center justify-center text-xs text-dim">
        No project open
      </div>
    )
  }

  const dimAction = 'no-drag flex items-center gap-1 text-dim hover:text-primary disabled:opacity-40 disabled:cursor-not-allowed'

  return (
    <div className="flex flex-col h-full text-xs bg-[#1a1b1e] px-3 py-3">
      {/* Row 1: harness card + status pill (branch-card idiom) */}
      <div className="shrink-0 flex items-center gap-2 mb-2">
        <div className="flex-1 min-w-0 h-9 px-3 rounded-lg border border-[#333639] bg-[#242629] flex items-center gap-2 overflow-hidden">
          <span className={'w-2 h-2 rounded-full shrink-0 ' + (running ? (thinking ? 'bg-[#e6c07b] animate-pulse' : 'bg-[#7dcf9e]') : 'bg-[#4a4f55]')} />
          <span className="truncate font-semibold text-white">{st?.harness || 'no harness'}</span>
          <span className="truncate shrink text-dim">ACP agent protocol</span>
        </div>
        {running ? (
          <button
            className="no-drag shrink-0 h-9 px-3 rounded-lg border border-[#e5735f]/40 bg-[#242629] text-[#e5735f] text-xs hover:border-[#e5735f]"
            onClick={() => void stop(activeId)}
          >
            ■ Stop
          </button>
        ) : null}
      </div>

      {!running && <ProviderSetup key={activeId} projectId={activeId} onStart={name => start(activeId, name)} />}
      {running && <ModelPicker key={`${activeId}:${st?.sessionId ?? ''}`} projectId={activeId} provider={st?.harness} thinking={thinking} onBusy={setModelBusy} />}

      {/* Row 2: small actions */}
      <div className="shrink-0 flex items-center gap-4 mb-2">
        <button className={dimAction} disabled={!running || thinking} title="Start a fresh session" onClick={() => newSession(activeId).catch((e) => setHistErr(String(e)))}>
          <span>＋</span> New Session
        </button>
        <button className={dimAction} disabled={timeline.length === 0} title="Delete the active session's transcript" onClick={() => clearTranscript(activeId).catch((e) => setHistErr(String(e)))}>
          ⧉ Clear transcript
        </button>
      </div>

      {/* Tab strip (segmented, git-style) */}
      <div className="shrink-0 flex items-stretch gap-1 p-1 rounded-lg border border-[#333639] bg-[#242629] mb-2">
        <button
          className={'flex-1 flex items-center justify-center gap-1.5 px-3 h-7 rounded-md text-xs ' + (tab === 'chat' ? 'bg-[#333639] text-white' : 'text-dim hover:text-white')}
          onClick={() => setTab('chat')}
        >
          Chat
          {counts.msgs + counts.tools > 0 && (
            <span className="rounded-full bg-[#3a3c3f] px-1.5 text-[10px] text-[#bcbec4]">{counts.msgs + counts.tools}</span>
          )}
        </button>
        <button
          className={'flex-1 flex items-center justify-center gap-1.5 px-3 h-7 rounded-md text-xs ' + (tab === 'history' ? 'bg-[#333639] text-white' : 'text-dim hover:text-white')}
          onClick={() => setTab('history')}
        >
          History
          {projSessions.length > 0 && (
            <span className="rounded-full bg-[#3a3c3f] px-1.5 text-[10px] text-[#bcbec4]">{projSessions.length}</span>
          )}
        </button>
        <button className="flex-1 flex items-center justify-center px-3 h-7 rounded-md text-xs text-[#4a4f55] cursor-not-allowed" title="Coming soon" disabled>
          Logs
        </button>
      </div>

      {/* Session-op errors surface in BOTH tabs (a Clear transcript refusal
          must be visible while the Chat tab is open too). */}
      {histErr && (
        <div className="shrink-0 bg-[#5a1d1d] text-[#ff9999] border border-[#ff6b6b]/30 rounded-lg px-3 py-1 mb-2">
          {histErr}
        </div>
      )}

      {tab === 'chat' ? (
        <div className="flex-1 min-h-0 relative">
          <div ref={listRef} onScroll={onScroll} className="absolute inset-0 overflow-y-auto py-1">
            {timeline.length === 0 && (
              <div className="pl-3 pr-3 h-7 flex items-center text-dim">
                {running ? 'No messages yet — say hello' : 'No messages yet — start a harness to chat'}
              </div>
            )}
            {timeline.map((item) => (
              <TimelineItemView
                key={item.type === 'message' ? item.id : item.type === 'tool' ? item.toolCallId : item.requestId}
                item={item}
                onRespond={(requestId, optionId, cancel) => void respondPermission(activeId, requestId, optionId, cancel)}
              />
            ))}
          </div>
          {showJump && (
            <button
              className="absolute bottom-2 left-1/2 -translate-x-1/2 px-3 py-1 rounded-full bg-[#242629] border border-[#333639] text-dim hover:text-primary text-[11px]"
              onClick={jumpToLatest}
            >
              ↓ jump to latest
            </button>
          )}
        </div>
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto">
          {projSessions.length === 0 && <div className="px-1 py-2 text-dim">No sessions yet</div>}
          {projSessions.map((s) => (
            <SessionRow
              key={s.id}
              s={s}
              active={s.id === activeSID}
              onOpen={() => {
                setHistErr(null)
                openSession(activeId, s.id).catch((e) => setHistErr(String(e)))
              }}
              onRename={(title) => {
                setHistErr(null)
                renameSession(s.id, title).catch((e) => setHistErr(String(e)))
              }}
              onDelete={() => setConfirmDelete(s)}
            />
          ))}
        </div>
      )}

      {/* Composer (commit-area idiom) */}
      {running && <div className="shrink-0 border-t border-[#333639] pt-3 mt-1 flex flex-col gap-2">
        <textarea
          ref={taRef}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value)
            grow()
          }}
          onKeyDown={onKeyDown}
          placeholder={running ? 'Ask the agent… (↑ history)' : 'Start a harness first'}
          disabled={!running || thinking || modelBusy}
          rows={2}
          className="no-drag w-full resize-none bg-[#1e1f22] border border-[#333639] rounded-lg p-3 font-mono text-[13px] text-primary outline-none focus:ring-1 focus:ring-[var(--accent)] placeholder:text-dim disabled:opacity-40"
        />
        <div className="flex items-center justify-between text-[10px]">
          <span className="text-dim">↑ history · Enter send · Shift+Enter newline</span>
          <span className={draft.length > 7000 ? 'text-[var(--modified)] font-medium' : 'text-dim'}>
            {draft.length} / 8000
          </span>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative flex-1 flex">
            <button
              disabled={!draft.trim() || thinking || modelBusy || !running}
              onClick={doSend}
              className="no-drag flex-1 h-9 px-3 rounded-l-lg bg-[#3b5bfd] text-white font-medium disabled:opacity-40 disabled:cursor-not-allowed"
            >
              Send ↵
            </button>
            <button
              disabled
              title="Send & queue (coming soon)"
              className="no-drag h-9 px-1.5 rounded-r-lg border border-l-0 border-[#333639] bg-[#242629] text-dim cursor-not-allowed"
            >
              ▾
            </button>
          </div>
        </div>
      </div>}

      {confirmDelete && (
        <ConfirmDialog
          title="Delete session"
          message={`Delete "${confirmDelete.title}"? This cannot be undone.`}
          confirmLabel="Delete"
          danger
          onConfirm={() => {
            if (activeId) {
              setHistErr(null)
              deleteSession(activeId, confirmDelete.id).then((err) => {
                if (err) setHistErr(err)
              })
            }
            setConfirmDelete(null)
          }}
          onCancel={() => setConfirmDelete(null)}
        />
      )}
    </div>
  )
}

export default AgentPanel
