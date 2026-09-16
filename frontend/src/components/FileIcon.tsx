import React from 'react'
import { extOf } from '../lib/filetype'

// Per-type file icons, 16px, all inline SVG (no icon dependency).
// Dev languages use small rounded color chips; config/doc types get
// bespoke glyphs. Unmapped extensions fall back to a gray doc glyph.

const CHIPS: Record<string, { label: string; bg: string; fg: string }> = {
  ts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  tsx: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  mts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  cts: { label: 'TS', bg: '#3178c6', fg: '#ffffff' },
  js: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  jsx: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  mjs: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  cjs: { label: 'JS', bg: '#f7df1e', fg: '#000000' },
  go: { label: 'GO', bg: '#00ADD8', fg: '#000000' },
  php: { label: 'PHP', bg: '#a071c9', fg: '#ffffff' },
  rs: { label: 'RS', bg: '#dea584', fg: '#000000' },
  py: { label: 'PY', bg: '#3572A5', fg: '#ffffff' },
  html: { label: 'HT', bg: '#e34c26', fg: '#ffffff' },
  htm: { label: 'HT', bg: '#e34c26', fg: '#ffffff' },
  css: { label: 'CS', bg: '#563d7c', fg: '#ffffff' },
  scss: { label: 'CS', bg: '#563d7c', fg: '#ffffff' },
  dockerfile: { label: 'D', bg: '#2496ed', fg: '#ffffff' },
}

const Chip: React.FC<{ label: string; bg: string; fg: string }> = ({
  label,
  bg,
  fg,
}) => (
  <span
    className="shrink-0 w-[14px] h-[14px] rounded-[3px] text-[8px] font-bold leading-none flex items-center justify-center"
    style={{ backgroundColor: bg, color: fg }}
  >
    {label}
  </span>
)

const JsonIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#cbcb41" strokeWidth="1.6" strokeLinecap="round">
    <path d="M6 2.5c-1.7 0-2.3.8-2.3 2.2v1.3c0 .9-.4 1.5-1.3 2 .9.5 1.3 1.1 1.3 2v1.3c0 1.4.6 2.2 2.3 2.2" />
    <path d="M10 2.5c1.7 0 2.3.8 2.3 2.2v1.3c0 .9.4 1.5 1.3 2-.9.5-1.3 1.1-1.3 2v1.3c0 1.4-.6 2.2-2.3 2.2" />
  </svg>
)

const MdIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16">
    <rect width="16" height="16" rx="3" fill="#6d80a2" />
    <text x="2" y="11.5" fontSize="8" fontWeight="700" fill="#ffffff" fontFamily="system-ui, sans-serif">M</text>
    <path d="M12.5 5v5.2M12.5 10.2l-1.7-1.7M12.5 10.2l1.7-1.7" stroke="#ffffff" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" fill="none" />
  </svg>
)

const EnvIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#e5c07b" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
    <path d="M2.5 7.5 8 3l5.5 4.5" />
    <path d="M4.2 7v6h7.6V7" />
  </svg>
)

const GitFlagIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
    <path d="M4.5 2v12" stroke="#e8703a" strokeWidth="1.5" strokeLinecap="round" />
    <path d="M4.5 3h7.5l-2.2 2.6 2.2 2.6H4.5z" fill="#e8703a" />
  </svg>
)

const YamlIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#519aba" strokeWidth="1.4" strokeLinecap="round">
    <circle cx="5" cy="4.5" r="1.6" />
    <circle cx="5" cy="11.5" r="1.6" />
    <circle cx="11" cy="6" r="1.6" />
    <path d="M5 6.1v3.8" />
    <path d="M11 7.6c0 2-1.5 2.7-3.5 2.9" />
  </svg>
)

const ShIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#89e051" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
    <path d="M4 4.5 8.5 8 4 11.5" />
    <path d="M10 11.5h2.5" />
  </svg>
)

const AstroIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#c084fc" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round">
    <path d="M8 1.8c2.1 1.7 3.1 4.2 3.1 6.7L9.4 10H6.6L4.9 8.5c0-2.5 1-5 3.1-6.7z" />
    <circle cx="8" cy="6.8" r="1.1" />
    <path d="M6.6 10.3 5.6 14l2.4-1.5L10.4 14l-1-3.7" />
  </svg>
)

const DocIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="#7a7f86" strokeWidth="1.3" strokeLinejoin="round">
    <path d="M3.5 1.5h6l3 3v10h-9z" />
    <path d="M9.5 1.5v3h3" />
  </svg>
)

const FolderIcon: React.FC<{ open?: boolean }> = ({ open }) => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinejoin="round">
    {open ? (
      <>
        <path d="M1.8 3.5h4l1.2 1.5h5.2v2.2" />
        <path d="M1.8 12.7 3.4 7h11L12.8 12.7z" />
      </>
    ) : (
      <path d="M1.8 3.5h4.2l1.3 1.6h6.9v8.1H1.8z" />
    )}
  </svg>
)

export const FileIcon: React.FC<{
  path: string
  dir?: boolean
  open?: boolean
}> = ({ path, dir, open }) => {
  if (dir) {
    return (
      <span
        className={
          'shrink-0 w-4 h-4 flex items-center justify-center ' +
          (open ? 'text-[#c8ccd0]' : 'text-[#8a8e94]')
        }
      >
        <FolderIcon open={open} />
      </span>
    )
  }
  const base = path.slice(path.lastIndexOf('/') + 1).toLowerCase()
  const ext = extOf(path)
  let icon: React.ReactNode
  if (base === 'dockerfile' || ext === 'dockerfile') {
    icon = <Chip label="D" bg="#2496ed" fg="#ffffff" />
  } else if (ext === 'env' || base.includes('.env') || base.startsWith('env.')) {
    icon = <EnvIcon />
  } else if (
    base === '.gitignore' || base === '.gitattributes' || base === '.gitmodules'
  ) {
    icon = <GitFlagIcon />
  } else if (ext === 'json' || ext === 'jsonc') {
    icon = <JsonIcon />
  } else if (ext === 'md' || ext === 'markdown') {
    icon = <MdIcon />
  } else if (ext === 'yaml' || ext === 'yml') {
    icon = <YamlIcon />
  } else if (ext === 'sh' || ext === 'bash' || ext === 'zsh') {
    icon = <ShIcon />
  } else if (ext === 'astro') {
    icon = <AstroIcon />
  } else {
    const chip = CHIPS[ext]
    icon = chip ? <Chip label={chip.label} bg={chip.bg} fg={chip.fg} /> : <DocIcon />
  }
  return (
    <span className="shrink-0 w-4 h-4 flex items-center justify-center">
      {icon}
    </span>
  )
}

export default FileIcon
