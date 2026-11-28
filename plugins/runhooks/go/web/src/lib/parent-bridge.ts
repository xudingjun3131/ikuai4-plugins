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

/** [v1.1.30] 设置项从插件页弹窗迁入工具箱后的状态快照(插件层拥有固件 API)。 */
export type DeviceEntry = {
  readonly ip?: string
  readonly mac?: string
  readonly name?: string
  readonly ip_addr?: string
  readonly hostname?: string
  readonly termname?: string
  readonly mac_addr?: string
}

export type SubEntry = {
  readonly id: string
  readonly name: string
  readonly url: string
  readonly format: string
  readonly enabled: boolean
  readonly interval_min?: number
  readonly host_count?: number
  readonly ip_count?: number
  readonly rule_count?: number
  readonly rewrite_count?: number
  readonly script_count?: number
  readonly skipped_count?: number
  readonly last_update?: number
  readonly last_error?: string
}

export type SettingsSnapshot = {
  readonly status: number
  readonly autostart: number
  readonly proxyPort: number
  readonly apiPort: number
  readonly transparentPort: number
  readonly geoLoaded: number
  readonly inWindow: number
  readonly lanDevices: readonly DeviceEntry[]
  readonly selectedDevices: readonly DeviceEntry[]
  readonly groups: readonly { readonly name: string; readonly devices: readonly DeviceEntry[] }[]
  readonly interfaces: string
  readonly ports: string
  readonly schedEnabled: boolean
  readonly schedStart: string
  readonly schedStop: string
  readonly schedDays: readonly number[]
  readonly weekDays: readonly { readonly v: number; readonly n: string }[]
  readonly domainMode: string
  readonly domains: string
  readonly dstIpMode: string
  readonly dstIps: string
  readonly geoMode: string
  readonly countries: string
  readonly recordMode: string
  readonly paths: string
  readonly methods: string
  readonly userAgents: string
  readonly ja3: string
  readonly contentTypes: string
  readonly sizeMin: string | number
  readonly sizeMax: string | number
  readonly subs: readonly SubEntry[]
  readonly subEffective: Record<string, unknown> | null
  readonly subEngine: boolean
  readonly subDetail: { readonly id: string; readonly parsed?: Record<string, unknown> } | null
  readonly subBusy: boolean
}

export function requestSettings(): void {
  parentCall("settings-request")
}

/** 保存一个分区: section 与 values 的键都在插件页白名单内。 */
export function applySettings(section: string, values: Record<string, unknown>): void {
  parentCall("settings-apply", { section, values })
}

/** 即时动作(刷新设备、订阅增删改、GEOIP 更新等), name 走插件页白名单。 */
export function settingsAction(name: string, args?: Record<string, unknown>): void {
  parentCall("settings-action", { name, args: args ?? {} })
}

