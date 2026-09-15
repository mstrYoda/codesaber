import React from 'react'
import { APP_VERSION } from '../version'
import {
  effectiveAccent,
  effectiveFontSize,
  useSettings,
  type Settings,
} from '../lib/settings'

// ToggleRow is a labelled switch row used across settings sections.
const ToggleRow: React.FC<{
  label: string
  desc: string
  on: boolean
  onToggle: () => void
}> = ({ label, desc, on, onToggle }) => (
  <div className="flex items-center justify-between gap-4 py-2">
    <div className="min-w-0">
      <div className="text-primary text-[12px]">{label}</div>
      <div className="text-dim text-[10px] mt-0.5">{desc}</div>
    </div>
    <button
      role="switch"
      aria-checked={on}
      aria-label={label}
      title={label}
      onClick={onToggle}
      className={
        'no-drag shrink-0 w-9 h-5 rounded-full relative transition-colors ' +
        (on ? 'bg-[var(--accent)]' : 'bg-[#3a3c3f]')
      }
    >
      <span
        className={
          'absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all ' +
          (on ? 'left-[18px]' : 'left-0.5')
        }
      />
    </button>
  </div>
)

const Section: React.FC<{ title: string; children: React.ReactNode }> = ({
  title,
  children,
}) => (
  <section className="mb-5">
    <h2 className="text-[10px] uppercase tracking-wide text-dim mb-1">
      {title}
    </h2>
    <div className="rounded-lg border border-[#333639] bg-[#242629] px-3 py-1">
      {children}
    </div>
  </section>
)

const ACCENTS: { name: string; hex: string }[] = [
  { name: 'Blue', hex: '#4a5bfc' },
  { name: 'Purple', hex: '#7c3aed' },
  { name: 'Green', hex: '#5dbb63' },
  { name: 'Orange', hex: '#d99a4e' },
]

const FONT_MIN = 11
const FONT_MAX = 18

// SettingsPanel is the Settings dock tab: editor, terminal, appearance and
// about sections. Every change auto-saves via SettingsPut (optimistic).
const SettingsPanel: React.FC = () => {
  const { settings, update } = useSettings()
  const fontPx = effectiveFontSize(settings)
  const accent = effectiveAccent(settings)

  const setFont = (px: number) =>
    update({ editorFontSizePx: Math.min(FONT_MAX, Math.max(FONT_MIN, px)) })

  const setAccent = (hex: string) =>
    update({ accentColor: hex === '#4a5bfc' ? '' : hex })

  return (
    <div className="flex flex-col h-full overflow-y-auto text-xs bg-[#1a1b1e] px-3 py-3">
      <Section title="Editor">
        <ToggleRow
          label="Bracket colorization"
          desc="Color nested ()[]{} pairs by depth"
          on={settings.bracketColors}
          onToggle={() => update({ bracketColors: !settings.bracketColors })}
        />
        <ToggleRow
          label="Minimap"
          desc="Show the code overview strip beside the scrollbar"
          on={settings.minimap}
          onToggle={() => update({ minimap: !settings.minimap })}
        />
        <ToggleRow
          label="Vim mode"
          desc="Normal/insert/visual modes with vim motions and operators"
          on={!!settings.vimMode}
          onToggle={() => update({ vimMode: !settings.vimMode })}
        />
        <div className="flex items-center justify-between gap-4 py-2">
          <div className="min-w-0">
            <div className="text-primary text-[12px]">Editor font size</div>
            <div className="text-dim text-[10px] mt-0.5">
              {FONT_MIN}–{FONT_MAX}px, default 13px
            </div>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            <button
              className="no-drag w-6 h-6 rounded border border-[#333639] text-dim hover:text-primary disabled:opacity-40 disabled:cursor-default"
              aria-label="Decrease editor font size"
              disabled={fontPx <= FONT_MIN}
              onClick={() => setFont(fontPx - 1)}
            >
              −
            </button>
            <span className="w-8 text-center text-primary font-mono">
              {fontPx}
            </span>
            <button
              className="no-drag w-6 h-6 rounded border border-[#333639] text-dim hover:text-primary disabled:opacity-40 disabled:cursor-default"
              aria-label="Increase editor font size"
              disabled={fontPx >= FONT_MAX}
              onClick={() => setFont(fontPx + 1)}
            >
              +
            </button>
          </div>
        </div>
      </Section>

      <Section title="Terminal">
        <div className="py-2">
          <div className="text-primary text-[12px] mb-1">Default shell</div>
          <input
            className="no-drag w-full h-8 px-2.5 rounded-lg border border-[#333639] bg-[#1e1f22] text-primary font-mono text-[11px] placeholder:text-dim outline-none focus:border-[#4a4f55]"
            placeholder="$SHELL or /bin/zsh"
            aria-label="Default shell"
            value={settings.terminalShell}
            onChange={(e) => update({ terminalShell: e.target.value })}
          />
          <div className="text-dim text-[10px] mt-1">
            Absolute path to the shell binary for new terminals. Empty uses
            your login shell ($SHELL, falling back to zsh). Applies to newly
            opened terminals.
          </div>
        </div>
      </Section>

      <Section title="Appearance">
        <div className="flex items-center justify-between gap-4 py-2">
          <div className="min-w-0">
            <div className="text-primary text-[12px]">Accent color</div>
            <div className="text-dim text-[10px] mt-0.5">
              Used for highlights, tabs and buttons
            </div>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            {ACCENTS.map((a) => (
              <button
                key={a.hex}
                className={
                  'no-drag w-5 h-5 rounded-full border-2 ' +
                  (accent.toLowerCase() === a.hex.toLowerCase()
                    ? 'border-white/80'
                    : 'border-transparent hover:border-white/40')
                }
                style={{ backgroundColor: a.hex }}
                title={a.name}
                aria-label={`Accent color ${a.name}`}
                aria-pressed={accent.toLowerCase() === a.hex.toLowerCase()}
                onClick={() => setAccent(a.hex)}
              />
            ))}
          </div>
        </div>
      </Section>

      <Section title="About">
        <div className="py-2 text-[11px] flex flex-col gap-1">
          <div className="flex justify-between gap-6">
            <span className="text-dim">Version</span>
            <span className="text-primary">codesaber {APP_VERSION}</span>
          </div>
          <div className="flex justify-between gap-6">
            <span className="text-dim">Runtime</span>
            <span className="text-primary">
              Wails 3 · {navigator.platform}
            </span>
          </div>
          <div className="flex justify-between gap-6">
            <span className="text-dim">Editor</span>
            <span className="text-primary">CodeMirror 6</span>
          </div>
        </div>
      </Section>
    </div>
  )
}

export type { Settings }
export default SettingsPanel
