import React, { useEffect, useMemo, type ReactNode } from 'react'
import { useGit, diffKey } from '../state/git'
import { parseDiffTabPath, type Tab } from '../state/tabs'
import { useProjects } from '../state/projects'

interface Line {
  op: '+' | '-' | ' '
  text: string
  oldNo?: number
  newNo?: number
}

// parseHunk parses "@@ -a,b +c,d @@" style headers into lines carrying
// two-gutter numbers. Structure changes (no "\ No newline" handling beyond a
// skip) — parse errors render plain lines via the fallback path.
const parseHunk = (startOld: number, startNew: number, lines: string[]): Line[] => {
  const out: Line[] = []
  let o = startOld
  let n = startNew
  for (const raw of lines) {
    if (raw.startsWith('\\')) continue
    const op = raw.slice(0, 1) as '+' | '-' | ' '
    const text = raw.slice(1)
    if (op === '+') {
      out.push({ op, text, newNo: n++ })
    } else if (op === '-') {
      out.push({ op, text, oldNo: o++ })
    } else {
      out.push({ op: ' ', text, oldNo: o++, newNo: n++ })
    }
  }
  return out
}

const HUNK_RE = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)?/

// --- Heuristic token colorizer (no new deps) -------------------------------
// Lights up keywords, strings, numbers and comments for common languages;
// anything unrecognized degrades to plain text silently.

const KEYWORDS = new Set([
  'abstract', 'and', 'any', 'as', 'async', 'await', 'bool', 'break', 'byte',
  'case', 'catch', 'chan', 'char', 'class', 'const', 'continue', 'defer',
  'default', 'do', 'dispatch', 'elif', 'else', 'enum', 'export', 'extends',
  'false', 'finally', 'float', 'for', 'from', 'func', 'function', 'go',
  'goto', 'if', 'impl', 'implements', 'import', 'in', 'instanceof', 'int',
  'interface', 'let', 'long', 'match', 'mod', 'namespace', 'new', 'nil',
  'None', 'not', 'null', 'or', 'package', 'pass', 'private', 'protected',
  'public', 'range', 'return', 'self', 'static', 'str', 'string', 'struct',
  'super', 'switch', 'template', 'this', 'throw', 'trait', 'true', 'try',
  'type', 'typeof', 'union', 'unsigned', 'use', 'using', 'var', 'void',
  'while', 'with', 'yield',
])

const TOKEN_RE =
  /(\/\/.*|#.*)|(\/\*[\s\S]*?\*\/|"""[\s\S]*?"""|'''[\s\S]*?''')|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`)|(\b\d+(?:\.\d+)?\b)|([A-Za-z_$][\w$]*)/g

const colorize = (text: string): ReactNode => {
  if (text.length > 2000) return text
  const out: ReactNode[] = []
  let last = 0
  let m: RegExpExecArray | null
  TOKEN_RE.lastIndex = 0
  let i = 0
  while ((m = TOKEN_RE.exec(text)) !== null && i < 500) {
    if (m.index > last) out.push(text.slice(last, m.index))
    const [full, lineC, blockC, str, num, word] = m
    let cls: string | null = null
    if (lineC || blockC) cls = 'text-[#5c6370] italic'
    else if (str) cls = 'text-[#98c379]'
    else if (num) cls = 'text-[#d7a35f]'
    else if (word && KEYWORDS.has(word)) cls = 'text-[#8aa6e8]'
    if (cls === null) out.push(full)
    else out.push(<span key={m.index} className={cls}>{full}</span>)
    last = m.index + full.length
    i++
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

const addedBg = 'rgba(13,140,50,0.13)'
const delBg = 'rgba(200,50,50,0.13)'

const Hunk: React.FC<{ header: string; lines: string[] }> = ({
  header,
  lines,
}) => {
  let body: Line[]
  try {
    const m = HUNK_RE.exec(header)
    if (!m) throw new Error('bad header')
    body = parseHunk(parseInt(m[1], 10), parseInt(m[2], 10), lines)
  } catch {
    body = lines.map((l) => ({ op: ' ' as const, text: l }))
  }
  return (
    <>
      <div className="px-0 py-0.5 font-mono text-dim bg-[#242629] text-[10px] tracking-wide select-none">
        {header}
      </div>
      {body.map((l, i) => (
        <div
          key={i}
          className={
            'flex ' +
            (l.op === '+' ? 'text-[#8ee0a0]' : l.op === '-' ? 'text-[#ef8070]' : '')
          }
          style={{
            backgroundColor:
              l.op === '+' ? addedBg : l.op === '-' ? delBg : undefined,
          }}
        >
          <span className="w-10 shrink-0 pr-1 text-dim bg-panel text-right font-mono select-none">
            {l.oldNo ?? ''}
          </span>
          <span className="w-10 shrink-0 pr-1 text-dim bg-panel text-right font-mono select-none">
            {l.newNo ?? ''}
          </span>
          <span className="w-3 shrink-0 select-none">{l.op === ' ' ? '' : l.op}</span>
          <span className="font-mono whitespace-pre pr-2">
            {colorize(l.text === '' ? ' ' : l.text)}
          </span>
        </div>
      ))}
    </>
  )
}

const DiffViewer: React.FC<{
  projectId: string
  tab: Tab
  active: boolean
}> = ({ projectId, tab, active }) => {
  const { diffs, diffVersion, fetchDiff } = useGit()
  const { projects } = useProjects()
  const parsed = parseDiffTabPath(tab.path)
  const key = parsed ? diffKey(projectId, parsed.path, parsed.staged) : ''
  const patch = key ? diffs[key] : undefined

  // (Re)fetch when the tab first renders or after cache invalidation; the
  // provider caches by key and bumps diffVersion on invalidation.
  useEffect(() => {
    if (key && !diffs[key]) fetchDiff(key)
  }, [key, diffs, diffVersion, fetchDiff])

  const additions = patch?.hunks?.reduce((a, h) => a + h.additions, 0) ?? 0
  const deletions = patch?.hunks?.reduce((a, h) => a + h.deletions, 0) ?? 0

  const dirs = useMemo(() => {
    const p = parsed?.path ?? ''
    const parts = p.split(/[\\/]/)
    return {
      name: parts[parts.length - 1],
      dir: parts.slice(0, parts.length - 1),
    }
  }, [parsed?.path])

  const projectName = projects.find((p) => p.id === projectId)?.name ?? projectId

  return (
    <div
      className="absolute inset-0 flex flex-col overflow-hidden"
      style={{ display: active ? 'flex' : 'none' }}
    >
      {/* Breadcrumb row: project > dirs… > FileName + staged/unstaged pill */}
      <div className="flex items-center gap-1 px-3 py-1 text-xs bg-panel border-b border-panel shrink-0">
        <span className="text-dim">{projectName}</span>
        <span className="text-dim">›</span>
        {dirs.dir.length > 2 ? (
          <>
            <span className="text-dim">{dirs.dir[0]}</span>
            <span className="text-dim">› … ›</span>
          </>
        ) : (
          <>
            {dirs.dir.map((d, idx) => (
              <span key={idx} className="shrink-0">
                <span className="text-dim">{d}</span>
                <span className="text-dim">›</span>
              </span>
            ))}
          </>
        )}
        <span className="text-primary truncate" title={parsed?.path}>
          {dirs.name}
        </span>
        <span
          className={
            'ml-auto shrink-0 px-2 py-[1px] rounded-full text-[10px] font-mono ' +
            (parsed?.staged
              ? 'bg-[rgba(74,91,252,0.15)] text-[var(--accent)]'
              : 'bg-[rgba(217,154,78,0.15)] text-[var(--modified)]')
          }
          title={parsed?.staged ? 'staged diff' : 'unstaged diff'}
        >
          {parsed?.staged ? 'STAGED' : 'UNSTAGED'}
        </span>
      </div>
      {/* Summary strip */}
      <div className="flex items-center gap-2 px-3 py-1 text-[10px] font-mono text-dim bg-panel border-b border-panel shrink-0">
        <span className="truncate" title={parsed?.path}>{parsed?.path}</span>
        <span className="ml-auto shrink-0">
          <span className="text-[var(--added)]">+{additions}</span>{' '}
          <span className="text-[var(--danger)]">−{deletions}</span>
        </span>
      </div>
      <div className="flex-1 overflow-auto" style={{ overflowX: 'auto' }}>
        {patch ? (
          patch.hunks && patch.hunks.length > 0 ? (
            patch.hunks.map((h, i) => (
              <div
                key={i}
                className="text-[11px] leading-5 min-w-max px-0"
              >
                <Hunk header={h.header} lines={h.lines ?? []} />
              </div>
            ))
          ) : (
            <div className="px-3 py-2 text-dim">No changes to show</div>
          )
        ) : (
          <div className="px-3 py-2 text-dim">Loading diff…</div>
        )}
      </div>
    </div>
  )
}

export default DiffViewer
