// requestCloseActive closes the active tab of the active project, but
// debounces requests within 200ms of each other. Both ⌘W paths (webview
// keydown and the native File → Close Tab menu accelerator) funnel through
// here, so whichever fires — sometimes both, if macOS hands the chord to the

import { CLOSE_DEBOUNCE_MS } from "./constants";

// webview *and* triggers the menu — only one tab is closed per press.
let lastCloseActiveAt = 0;
const requestCloseActive = (close: () => void) => {
  const now = Date.now();
  if (now - lastCloseActiveAt < CLOSE_DEBOUNCE_MS) return;
  lastCloseActiveAt = now;
  close();
};

/**
 * railBtn returns the className for a button in the rail, given whether it is active or not.
 * */
const railBtn = (active: boolean) =>
  "no-drag relative w-7 h-7 rounded-md flex items-center justify-center " +
  (active
    ? "text-primary bg-white/10 after:absolute after:left-[-6px] after:top-1 after:bottom-1 after:w-0.5 after:bg-[var(--accent)] after:rounded-full"
    : "text-dim hover:text-primary hover:bg-white/8");

export { requestCloseActive, railBtn };
