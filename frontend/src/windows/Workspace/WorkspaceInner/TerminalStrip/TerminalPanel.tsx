import React, { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { Events } from '@wailsio/runtime'
import '@xterm/xterm/css/xterm.css'
import { App } from '../../../../../bindings/codesaber/backend';
import { b64ToBytes, bytesToB64 } from '../../../../state/terminal';

interface Props {
  termId: string
  visible: boolean
}

const TerminalPanel: React.FC<Props> = ({ termId, visible }) => {
  const containerRef = useRef<HTMLDivElement>(null)
  // doFit lives in a ref so the visibility effect below can trigger a refit
  // when the tab becomes active without recreating the xterm instance.
  const fitRef = useRef<() => void>(() => {})

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new Terminal({
      fontSize: 12,
      fontFamily: '"SF Mono", Menlo, Monaco, monospace',
      cursorBlink: false,
      scrollback: 5000,
      theme: {
        background: '#1e1f22',
        foreground: '#bcbec4',
        brightBlack: '#73767b',
        blue: '#4a5bfc',
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.loadAddon(new WebLinksAddon())
    term.open(container)

    // xterm needs a real-sized DOM node before fitting; rAF lets layout
    // settle after the container mounts. Hidden tabs skip fitting.
    let rafId = 0
    let lastRows = 0
    let lastCols = 0
    const doFit = () => {
      try {
        const proposed = fit.proposeDimensions()
        if (!proposed) return
        if (proposed.rows < 2 || proposed.cols < 2) return
        fit.fit()
        if (term.rows !== lastRows || term.cols !== lastCols) {
          lastRows = term.rows
          lastCols = term.cols
          void App.TermResize(termId, term.rows, term.cols).catch(() => {})
        }
      } catch {
        // container may be zero-sized mid-layout
      }
    }
    fitRef.current = doFit

    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(rafId)
      rafId = requestAnimationFrame(doFit)
    })
    ro.observe(container)
    rafId = requestAnimationFrame(doFit)

    const offData = Events.On('term.data', (ev: any) => {
      const { termId: t, data } = (ev.data ?? {}) as {
        termId?: string
        data?: string
      }
      if (t !== termId || !data) return
      term.write(b64ToBytes(data))
    })

    let disposed = false
    const dataSub = term.onData((d) => {
      if (disposed) return
      void App.TermInput(
        termId,
        bytesToB64(new TextEncoder().encode(d)),
      ).catch(() => {})
    })

    return () => {
      disposed = true
      cancelAnimationFrame(rafId)
      ro.disconnect()
      offData()
      dataSub.dispose()
      term.dispose()
    }
  }, [termId])

  useEffect(() => {
    if (!visible) return
    // Refit after the container becomes visible again.
    requestAnimationFrame(() => fitRef.current())
  }, [visible])

  return (
    <div
      ref={containerRef}
      className="h-full w-full p-1"
      style={{ display: visible ? undefined : 'none' }}
    />
  )
}

export default TerminalPanel
