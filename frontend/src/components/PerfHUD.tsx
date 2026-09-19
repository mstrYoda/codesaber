import React, { useEffect, useState } from 'react'
import * as App from '../../bindings/codesaber/backend/app'
import { Health } from '../../bindings/codesaber/backend/models'
import { onSettingsChange, perfHudEnabled, togglePerfHud } from '../lib/settings'
import { useKeyboardShortcut } from '../state/keybinding'

const Row: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="flex justify-between gap-6">
    <span className="text-dim">{label}</span>
    <span>{value}</span>
  </div>
)

interface Sample {
  rpcMs: number
  health?: Health
}

// PerfHUD is the ⌘⇧H overlay: a small mono card sampling backend health at
// 1Hz. rpcMs measures the full JS→Go→JS roundtrip of App.HealthMetrics.
const PerfHUD: React.FC = () => {
  const [enabled, setEnabled] = useState(perfHudEnabled())
  const [sample, setSample] = useState<Sample | null>(null)

  useEffect(() => onSettingsChange(() => setEnabled(perfHudEnabled())), [])

  useKeyboardShortcut('perfHud', () => togglePerfHud())

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    const tick = async () => {
      const t0 = performance.now()
      try {
        const health = await App.HealthMetrics()
        if (cancelled) return
        setSample({ rpcMs: performance.now() - t0, health })
      } catch {
        if (!cancelled) setSample({ rpcMs: performance.now() - t0 })
      }
    }
    void tick()
    const id = window.setInterval(() => void tick(), 1000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [enabled])

  if (!enabled || !sample) return null
  const h = sample.health

  return (
    <div
      className="fixed top-8 right-3 z-60 rounded-lg bg-black/85 backdrop-blur border border-[#3a3d45] text-mono text-[11px] leading-5 px-3 py-2 min-w-[190px] shadow-lg select-none"
      data-testid="perf-hud"
    >
      <div className="text-dim mb-1 tracking-wide">codesaber perf</div>
      <Row label="rpc" value={`${sample.rpcMs.toFixed(1)}ms`} />
      <Row label="rss" value={`${(h?.rssMB ?? 0).toFixed(1)}MB`} />
      <Row label="goroutines" value={String(h?.goroutines ?? 0)} />
      <Row label="watchers" value={String(h?.watchers ?? 0)} />
      <Row label="terminals" value={String(h?.terminals ?? 0)} />
      <Row label="agent sessions" value={String(h?.acpSessions ?? 0)} />
      <Row label="buffers" value={String(h?.editorBuffers ?? 0)} />
    </div>
  )
}

export default PerfHUD
