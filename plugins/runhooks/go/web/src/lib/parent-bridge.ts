/**
 * Bridge from the embedded WebUI (iframe) back to the plugin shell page.
 *
 * The shell owns the plugin-framework actions (capture master switch, settings
 * dialog, fullscreen). Those are not reachable from the engine REST API, so the
 * WebUI delegates them via postMessage instead of duplicating buttons in the
 * shell. The shell also pushes its status tags here so the ribbon can show
 * them without the shell taking up vertical space.
 */
export type ParentState = {
  readonly status: number
  readonly capture: number
  readonly rulesOn: number
  readonly caok: number
  readonly flows: number
  readonly enginePaused: number
  readonly fullscreen: boolean
  readonly settingsOpen: boolean
}

export const DEFAULT_STATE: ParentState = {
  status: 0,
  capture: 0,
  rulesOn: 0,
  caok: 0,
  flows: 0,
  enginePaused: 0,
  fullscreen: false,
  settingsOpen: false,
}

/** Standalone embedding (dev server) simply no-ops. */
export function parentCall(action: string, payload?: Record<string, unknown>): void {
  if (typeof window === "undefined" || window.parent === window) return
  window.parent.postMessage({ __netloom: 1, action, ...(payload ?? {}) }, "*")
}
