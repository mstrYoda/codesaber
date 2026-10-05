import React from 'react'

// Language chips: 18px filled square per extension (design-mockup palette).
// Shared by GitPanel rows and the Titlebar breadcrumb.
const LANGS: Record<string, { label: string; bg: string; fg: string }> = {
  ts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  tsx: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  mts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  cts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  js: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  jsx: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  mjs: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  cjs: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  go: { label: 'GO', bg: '#00ADD8', fg: '#000000' },
  rs: { label: 'RS', bg: '#dea584', fg: '#000000' },
  py: { label: 'PY', bg: '#3572A5', fg: '#ffffff' },
  html: { label: 'HT', bg: '#e34c26', fg: '#ffffff' },
  htm: { label: 'HT', bg: '#e34c26', fg: '#ffffff' },
  css: { label: 'CS', bg: '#563d7c', fg: '#ffffff' },
  scss: { label: 'CS', bg: '#563d7c', fg: '#ffffff' },
  json: { label: 'JS', bg: '#999999', fg: '#000000' },
  md: { label: 'MD', bg: '#6d80a2', fg: '#ffffff' },
  sh: { label: 'SH', bg: '#89e051', fg: '#000000' },
  bash: { label: 'SH', bg: '#89e051', fg: '#000000' },
  zsh: { label: 'SH', bg: '#89e051', fg: '#000000' },
}

// Binary extensions: never diff-stat these; show a human size instead.
const BINARY_EXTS = new Set([
  'wasm', 'png', 'jpg', 'jpeg', 'gif', 'ico', 'webp', 'pdf', 'exe', 'dll',
  'bin', 'svg',
])

export const extOf = (p: string) => {
  const base = p.slice(Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\')) + 1)
  const dot = base.lastIndexOf('.')
  return dot === -1 ? '' : base.slice(dot + 1).toLowerCase()
}

export const isBinaryPath = (p: string) => BINARY_EXTS.has(extOf(p))

// humanizeSize renders compact byte counts: 18 KB, 1.4 MB, 2.31 GB.
export const humanizeSize = (n: number) => {
  if (n < 1024) return `${n} B`
  const kb = n / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  if (mb < 1024) return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
  return `${(mb / 1024).toFixed(2)} GB`
}

const FileGlyph: React.FC = () =>
  React.createElement(
    'span',
    {
      className:
        'shrink-0 w-[18px] h-[18px] rounded-[4px] flex items-center justify-center text-dim',
    },
    React.createElement(
      'svg',
      { width: 10, height: 12, viewBox: '0 0 10 12', fill: 'none' },
      React.createElement('path', {
        d: 'M1 1h5l3 3v7H1V1z',
        stroke: 'currentColor',
        strokeWidth: 1,
        strokeLinejoin: 'round',
      }),
      React.createElement('path', {
        d: 'M6 1v3h3',
        stroke: 'currentColor',
        strokeWidth: 1,
        strokeLinejoin: 'round',
      }),
    ),
  )

export const langChip = (path: string): React.ReactElement => {
  const ext = extOf(path)
  if (BINARY_EXTS.has(ext)) {
    return React.createElement(
      'span',
      {
        className:
          'shrink-0 h-[14px] px-[3px] rounded-[4px] bg-[#7c3aed] text-white text-[8px] font-bold leading-[14px] text-center min-w-[18px]',
      },
      'BIN',
    )
  }
  const l = LANGS[ext]
  if (!l) return React.createElement(FileGlyph)
  return React.createElement(
    'span',
    {
      className:
        'shrink-0 w-[18px] h-[18px] rounded-[4px] text-[9px] font-bold flex items-center justify-center',
      style: { backgroundColor: l.bg, color: l.fg },
    },
    l.label,
  )
}
