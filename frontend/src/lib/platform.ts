export const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform)

/** Render the same Mod shortcut using the current desktop's key names. */
export const shortcut = (keys: string) =>
  isMac() ? `⌘${keys.replace(/Shift\+/g, '⇧').replace(/Alt\+/g, '⌥')}` : `Ctrl+${keys}`

export const terminalLabel = (shell: string, sequence: number) => {
  const name = shell.trim().split(/[\\/]/).pop()?.replace(/\.exe$/i, '')
  return `${name || 'Terminal'} ${sequence}`
}
