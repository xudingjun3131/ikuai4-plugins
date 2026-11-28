import { useCallback, useEffect, useState, type ReactNode } from "react"
import {
  BookOpen, Check, Download, Eye, Filter, Info, Monitor, Network,
  Pause, Play, Plus, RefreshCw, Rss, Save, Server, Settings, Shield, Trash2, X,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  applySettings, requestSettings, settingsAction,
  type SettingsSnapshot, type SubEntry,
} from "@/lib/parent-bridge"
import { cn } from "@/lib/utils"

/* [v1.1.30] 设置项从插件页弹窗迁入运维工具箱。
   不是把原 DOM 搬过来: 按工具箱内部布局重排为「左分区导航 + 右卡片表单」,
   每个分区独立保存; 固件 API 只存在于插件页, 全部读写经 postMessage 桥接。 */

type SectionId = "service" | "help" | "devices" | "network" | "decrypt" | "record" | "subs"

const SECTIONS: { id: SectionId; label: string; hint: string; icon: typeof Settings }[] = [
  { id: "service", label: "服务与端口", hint: "开机自启与三个监听端口", icon: Server },
  { id: "help", label: "使用说明", hint: "观测步骤与范围说明", icon: BookOpen },
  { id: "devices", label: "观测设备", hint: "在线终端、手动添加与分组", icon: Monitor },
  { id: "network", label: "网络范围", hint: "接口、端口与定时抓包", icon: Network },
  { id: "decrypt", label: "解密范围", hint: "域名、目标 IP 与国家地区", icon: Shield },
  { id: "record", label: "记录范围", hint: "路径、方法、UA、JA3 与大小", icon: Filter },
  { id: "subs", label: "规则订阅", hint: "QuanX / Surge / 纯列表", icon: Rss },
]

function useSettingsSnapshot(): SettingsSnapshot | null {
  const [snap, setSnap] = useState<SettingsSnapshot | null>(null)
  useEffect(() => {
    const onMessage = (e: MessageEvent): void => {
      if (e.source !== window.parent) return
      const d = e.data as Record<string, unknown> | null
      if (!d || d.__netloom !== 1 || d.type !== "settings") return
      setSnap(d.settings as SettingsSnapshot)
    }
    window.addEventListener("message", onMessage)
    requestSettings()
    return () => window.removeEventListener("message", onMessage)
  }, [])
  return snap
}

/* ---------- 通用小组件(工具箱视觉语言: 卡片 / 分区标题 / 栅格表单) ---------- */

function Card({ title, desc, children, action }: {
  title: string; desc?: string; children: ReactNode; action?: ReactNode
}) {
  return (
    <section className="nl-set-card">
      <div className="nl-set-card-head">
        <div>
          <h3>{title}</h3>
          {desc ? <p>{desc}</p> : null}
        </div>
        {action}
      </div>
      <div className="nl-set-card-body">{children}</div>
    </section>
  )
}

function Field({ label, hint, children, wide }: {
  label: string; hint?: string; children: ReactNode; wide?: boolean
}) {
  return (
    <label className={cn("nl-set-field", wide && "nl-set-field--wide")}>
      <span className="nl-set-label">{label}</span>
      {children}
      {hint ? <span className="nl-set-hint">{hint}</span> : null}
    </label>
  )
}

function SaveBar({ onClick, label = "保存", disabled }: {
  onClick: () => void; label?: string; disabled?: boolean
}) {
  return (
    <div className="nl-set-savebar">
      <Button size="sm" onClick={onClick} disabled={disabled} className="gap-1.5">
        <Save size={13} /> {label}
      </Button>
      <span className="nl-set-hint">保存后由插件页写入设备并热重载，右侧会回读最新值</span>
    </div>
  )
}

function ModeSelect({ value, onChange, options }: {
  value: string; onChange: (v: string) => void
  options: { value: string; label: string }[]
}) {
  return (
    <select className="nl-set-input" value={value} onChange={(e) => onChange(e.target.value)}>
      {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
    </select>
  )
}

const MODE2 = [
  { value: "all_except", label: "全部（除外）" },
  { value: "only", label: "仅命中" },
]
const MODE3 = [
  { value: "off", label: "关闭" },
  { value: "only", label: "仅这些国家" },
  { value: "except", label: "排除这些国家" },
]

/* ---------- 主组件 ---------- */

export function SettingsPanel() {
  const snap = useSettingsSnapshot()
  const [section, setSection] = useState<SectionId>("service")
  const [draft, setDraft] = useState<Partial<SettingsSnapshot>>({})
  const [manual, setManual] = useState({ ip: "", mac: "", name: "" })
  const [groupName, setGroupName] = useState("")
  const [subForm, setSubForm] = useState({ url: "", name: "", format: "auto", interval: 0 })

  useEffect(() => { if (snap) setDraft(snap) }, [snap])

  const set = useCallback(<K extends keyof SettingsSnapshot>(k: K, v: SettingsSnapshot[K]) => {
    setDraft((prev) => ({ ...prev, [k]: v }))
  }, [])

  const scopeValues = (): Record<string, unknown> => ({
    domainMode: draft.domainMode ?? "all_except", domains: draft.domains ?? "",
    dstIpMode: draft.dstIpMode ?? "all_except", dstIps: draft.dstIps ?? "",
    geoMode: draft.geoMode ?? "off", countries: draft.countries ?? "",
    recordMode: draft.recordMode ?? "all_except", paths: draft.paths ?? "",
    methods: draft.methods ?? "", userAgents: draft.userAgents ?? "",
    ja3: draft.ja3 ?? "", contentTypes: draft.contentTypes ?? "",
    sizeMin: draft.sizeMin ?? "", sizeMax: draft.sizeMax ?? "",
  })

  return (
    <div className="nl-set" data-testid="settings-panel">
      <aside className="nl-set-nav">
        <div className="nl-set-nav-title">设置分区</div>
        {SECTIONS.map((s) => {
          const Icon = s.icon
          return (
            <button
              key={s.id}
              type="button"
              className={cn("nl-set-nav-item", section === s.id && "nl-set-nav-item--active")}
              onClick={() => setSection(s.id)}
              data-testid={`settings-nav-${s.id}`}
            >
              <Icon size={14} />
              <span>
                <b>{s.label}</b>
                <small>{s.hint}</small>
              </span>
            </button>
          )
        })}
        <div className="nl-set-nav-foot">
          <Info size={12} /> 配置由插件页保存，工具箱只做展示与交互
        </div>
      </aside>

      <div className="nl-set-content">
        {!snap ? (
          <div className="nl-set-loading">正在从插件页读取配置…（若引擎未启动，请先在上方开启抓包）</div>
        ) : null}

        <div className="nl-set-head">
          <h2>{SECTIONS.find((s) => s.id === section)?.label}</h2>
          <span>{SECTIONS.find((s) => s.id === section)?.hint}</span>
          <span className="nl-set-head-status">
            <Badge variant="outline" className="text-[10px]">
              引擎 {Number(snap?.status) === 1 ? "运行中" : "未启动"}
            </Badge>
          </span>
        </div>

        {section === "service" ? (
          <>
            <Card title="引擎启动" desc="引擎随观测台顶部的抓包总开关自动拉起，无需单独启停">
              <label className="nl-set-switch-row">
                <input
                  type="checkbox"
                  checked={Number(draft.autostart) === 1}
                  onChange={(e) => {
                    const v = e.target.checked ? 1 : 0
                    set("autostart", v)
                    applySettings("autostart", { autostart: v })
                  }}
                  data-testid="settings-autostart"
                />
                <span>开机自启</span>
                <small>设备重启后自动拉起抓包引擎</small>
              </label>
            </Card>
            <Card title="监听端口" desc="修改端口会自动重启引擎；设备代理填「路由器IP:显式代理端口」">
              <div className="nl-set-grid">
                <Field label="显式代理" hint="浏览器 / 设备手动代理">
                  <input className="nl-set-input" type="number" value={draft.proxyPort ?? 18060}
                    onChange={(e) => set("proxyPort", Number(e.target.value))} />
                </Field>
                <Field label="API / 界面" hint="观测台与 CA 下载">
                  <input className="nl-set-input" type="number" value={draft.apiPort ?? 18061}
                    onChange={(e) => set("apiPort", Number(e.target.value))} />
                </Field>
                <Field label="透明监听" hint="iptables REDIRECT 入口">
                  <input className="nl-set-input" type="number" value={draft.transparentPort ?? 18062}
                    onChange={(e) => set("transparentPort", Number(e.target.value))} />
                </Field>
              </div>
              <SaveBar label="保存端口" onClick={() => applySettings("ports", {
                proxyPort: draft.proxyPort, apiPort: draft.apiPort, transparentPort: draft.transparentPort,
              })} />
            </Card>
          </>
        ) : null}

        {section === "help" ? (
          <Card title="观测步骤" desc="按顺序配置即可开始抓包，数据全部在网关本地处理">
            <ol className="nl-set-steps">
              <li>在「服务与端口」确认端口，勾开机自启；引擎随抓包开关自动拉起。</li>
              <li>在「观测设备」勾选要观测的终端（不选＝不抓任何设备，绝不全局）。</li>
              <li>用顶部「继续记录」开始／暂停，与总开关等效。</li>
              <li>在 ribbon 点「CA 证书」下载并在目标设备安装、信任，否则 HTTPS 站点会报证书错误。</li>
              <li>在「规则订阅」粘贴 QuanX / Surge / 纯列表地址，自动适配解密范围与改写规则。</li>
              <li>回到总览、设备星图、会话瀑布、域名洞察查看结果；「全屏观测台」在本页内铺满。</li>
              <li>断点改包、单请求编写、脚本调试都在运维工具箱其它分区。</li>
            </ol>
            <p className="nl-set-note">
              解密范围 / 记录范围可按域名、目标 IP、国家地区、路径、方法、UA、JA3、内容类型、大小精细过滤；
              路由器自身流量已排除。HSTS 站点必须信任 CA 才能正常打开。
            </p>
          </Card>
        ) : null}

        {section === "devices" ? (
          <>
            <Card title="已选设备" desc={`当前 ${(draft.selectedDevices ?? []).length} 台；不选＝不抓任何设备`}
              action={<Button size="sm" variant="outline" className="h-7 gap-1 text-[11px]"
                onClick={() => settingsAction("refresh-devices")} data-testid="settings-devices-refresh">
                <RefreshCw size={12} /> 刷新列表</Button>}>
              <div className="nl-set-chips">
                {(draft.selectedDevices ?? []).length === 0
                  ? <span className="nl-set-empty">暂无已选设备</span>
                  : (draft.selectedDevices ?? []).map((d, i) => (
                    <span className="nl-set-chip" key={`${d.ip}-${i}`}>
                      <b>{d.name || d.ip}</b>
                      {d.mac ? <small>{d.mac}</small> : null}
                      <button type="button" onClick={() => settingsAction("remove-selected", { index: i })} aria-label="移除"><X size={11} /></button>
                    </span>
                  ))}
              </div>
              <div className="nl-set-chips nl-set-chips--groups">
                {(draft.groups ?? []).length === 0 ? <span className="nl-set-empty">暂无分组</span> :
                  (draft.groups ?? []).map((g) => (
                    <span className="nl-set-chip nl-set-chip--group" key={g.name}>
                      <button type="button" onClick={() => settingsAction("apply-group", { name: g.name })}>{g.name}（{g.devices?.length ?? 0}）</button>
                      <button type="button" onClick={() => settingsAction("del-group", { name: g.name })} aria-label="删除分组"><Trash2 size={11} /></button>
                    </span>
                  ))}
              </div>
            </Card>
            <Card title="在线设备" desc="列表来自 DHCP 租约 + ARP 表；静态 IP 设备需先与路由器通信过">
              <div className="nl-set-devices">
                {(draft.lanDevices ?? []).length === 0 ? (
                  <span className="nl-set-empty">暂无在线设备，可在下方手动添加</span>
                ) : (draft.lanDevices ?? []).map((d, i) => {
                  const checked = (draft.selectedDevices ?? []).some(
                    (s) => (d.mac && s.mac === d.mac) || (d.ip_addr && s.ip === d.ip_addr),
                  )
                  return (
                    <label className={cn("nl-set-device", checked && "nl-set-device--on")} key={`${d.ip_addr}-${i}`}>
                      <input type="checkbox" checked={checked}
                        onChange={() => settingsAction("toggle-device", { device: d })} />
                      <b>{d.hostname || d.termname || "未命名设备"}</b>
                      <span className="nl-set-mono">{d.ip_addr}</span>
                      <span className="nl-set-mono">{d.mac}</span>
                      {checked ? <Check size={13} className="nl-set-check" /> : null}
                    </label>
                  )
                })}
              </div>
            </Card>
            <Card title="手动添加与分组" desc="填 IP 即可添加；存为分组会把当前已选设备一起保存">
              <div className="nl-set-grid">
                <Field label="IP"><input className="nl-set-input" placeholder="192.168.9.23" value={manual.ip}
                  onChange={(e) => setManual({ ...manual, ip: e.target.value })} /></Field>
                <Field label="MAC（可选）"><input className="nl-set-input" placeholder="可选" value={manual.mac}
                  onChange={(e) => setManual({ ...manual, mac: e.target.value })} /></Field>
                <Field label="名称（可选）"><input className="nl-set-input" placeholder="可选" value={manual.name}
                  onChange={(e) => setManual({ ...manual, name: e.target.value })} /></Field>
              </div>
              <div className="nl-set-row">
                <Button size="sm" className="gap-1.5" disabled={!manual.ip}
                  onClick={() => { settingsAction("add-manual", manual); setManual({ ip: "", mac: "", name: "" }) }}>
                  <Plus size={13} /> 添加并保存
                </Button>
              </div>
              <div className="nl-set-row">
                <input className="nl-set-input" style={{ maxWidth: 200 }} placeholder="分组名" value={groupName}
                  onChange={(e) => setGroupName(e.target.value)} />
                <Button size="sm" variant="outline" className="gap-1.5" disabled={!groupName}
                  onClick={() => { applySettings("group", { groupName }); setGroupName("") }}>
                  <Save size={13} /> 存为分组
                </Button>
              </div>
            </Card>
          </>
        ) : null}

        {section === "network" ? (
          <>
            <Card title="接口与端口" desc="留空＝全部接口；端口默认 80,443">
              <div className="nl-set-grid">
                <Field label="接口 / VLAN" hint="如 lan1,lan2">
                  <input className="nl-set-input" value={draft.interfaces ?? ""}
                    onChange={(e) => set("interfaces", e.target.value)} />
                </Field>
                <Field label="端口" hint="逗号分隔">
                  <input className="nl-set-input" value={draft.ports ?? "80,443"}
                    onChange={(e) => set("ports", e.target.value)} />
                </Field>
              </div>
              <SaveBar label="保存网络范围" onClick={() => applySettings("network", {
                interfaces: draft.interfaces ?? "", ports: draft.ports ?? "80,443",
              })} />
            </Card>
            <Card title="定时抓包" desc="按路由器本地时间生效"
              action={<Badge variant="outline" className="text-[10px]">
                {Number(draft.inWindow) === 1 ? "当前在时段内" : "当前不在时段内"}</Badge>}>
              <label className="nl-set-switch-row">
                <input type="checkbox" checked={Boolean(draft.schedEnabled)}
                  onChange={(e) => set("schedEnabled", e.target.checked)} />
                <span>启用定时抓包</span>
              </label>
              <div className="nl-set-grid">
                <Field label="开始"><input className="nl-set-input" placeholder="09:00" value={draft.schedStart ?? "09:00"}
                  onChange={(e) => set("schedStart", e.target.value)} /></Field>
                <Field label="结束"><input className="nl-set-input" placeholder="18:00" value={draft.schedStop ?? "18:00"}
                  onChange={(e) => set("schedStop", e.target.value)} /></Field>
              </div>
              <div className="nl-set-row">
                <span className="nl-set-label">快捷</span>
                <Button size="sm" variant="outline" className="h-7 text-[11px]"
                  onClick={() => set("schedDays", (draft.weekDays ?? []).map((d) => d.v))}>每天</Button>
                <Button size="sm" variant="outline" className="h-7 text-[11px]"
                  onClick={() => set("schedDays", (draft.weekDays ?? []).filter((d) => d.v <= 5).map((d) => d.v))}>工作日</Button>
                <Button size="sm" variant="outline" className="h-7 text-[11px]"
                  onClick={() => set("schedDays", (draft.weekDays ?? []).filter((d) => d.v >= 6).map((d) => d.v))}>周末</Button>
                <span className="nl-set-hint">已选 {(draft.schedDays ?? []).length} 天</span>
              </div>
              <div className="nl-set-chips">
                {(draft.weekDays ?? []).map((d) => {
                  const on = (draft.schedDays ?? []).includes(d.v)
                  return (
                    <button key={d.v} type="button" className={cn("nl-set-day", on && "nl-set-day--on")}
                      onClick={() => {
                        const cur = draft.schedDays ?? []
                        set("schedDays", on ? cur.filter((v) => v !== d.v) : [...cur, d.v])
                      }}>{d.n}</button>
                  )
                })}
              </div>
              <SaveBar label="保存时段" onClick={() => applySettings("schedule", {
                schedEnabled: draft.schedEnabled ?? false, schedStart: draft.schedStart ?? "09:00",
                schedStop: draft.schedStop ?? "18:00", schedDays: draft.schedDays ?? [],
              })} />
            </Card>
          </>
        ) : null}

        {section === "decrypt" ? (
          <>
            <Card title="域名" desc="命中规则的主机才做 HTTPS 解密">
              <div className="nl-set-grid">
                <Field label="模式"><ModeSelect value={draft.domainMode ?? "all_except"} options={MODE2}
                  onChange={(v) => set("domainMode", v)} /></Field>
                <Field label="域名列表" hint="逗号分隔，支持 * 通配" wide>
                  <input className="nl-set-input" placeholder="*.taobao.com,api.github.com"
                    value={draft.domains ?? ""} onChange={(e) => set("domains", e.target.value)} />
                </Field>
              </div>
            </Card>
            <Card title="目标 IP" desc="CIDR 或单 IP">
              <div className="nl-set-grid">
                <Field label="模式"><ModeSelect value={draft.dstIpMode ?? "all_except"} options={MODE2}
                  onChange={(v) => set("dstIpMode", v)} /></Field>
                <Field label="IP 列表" hint="逗号分隔" wide>
                  <input className="nl-set-input" placeholder="203.0.113.0/24,198.51.100.7"
                    value={draft.dstIps ?? ""} onChange={(e) => set("dstIps", e.target.value)} />
                </Field>
              </div>
            </Card>
            <Card title="国家地区" desc="需先下载 GEOIP 数据"
              action={<Badge variant="outline" className="text-[10px]">
                {Number(draft.geoLoaded) === 1 ? "GEOIP 已加载" : "GEOIP 未下载"}</Badge>}>
              <div className="nl-set-grid">
                <Field label="模式"><ModeSelect value={draft.geoMode ?? "off"} options={MODE3}
                  onChange={(v) => set("geoMode", v)} /></Field>
                <Field label="国家码" hint="两位国别码，逗号分隔" wide>
                  <input className="nl-set-input" placeholder="CN,US" value={draft.countries ?? ""}
                    onChange={(e) => set("countries", e.target.value)} />
                </Field>
              </div>
              <div className="nl-set-row">
                <Button size="sm" variant="outline" className="gap-1.5" onClick={() => settingsAction("geoip-update")}>
                  <Download size={13} /> 更新 GEOIP 数据
                </Button>
              </div>
              <SaveBar label="保存解密范围" onClick={() => applySettings("scope", scopeValues())} />
            </Card>
          </>
        ) : null}

        {section === "record" ? (
          <Card title="记录过滤" desc="下面任一项命中即视为「命中」；留空＝不限">
            <div className="nl-set-grid">
              <Field label="匹配模式"><ModeSelect value={draft.recordMode ?? "all_except"} options={MODE2}
                onChange={(v) => set("recordMode", v)} /></Field>
              <Field label="路径正则" hint="逗号分隔" wide>
                <input className="nl-set-input" placeholder="^/api/,^/v[0-9]+/" value={draft.paths ?? ""}
                  onChange={(e) => set("paths", e.target.value)} /></Field>
              <Field label="方法" hint="POST,PUT">
                <input className="nl-set-input" value={draft.methods ?? ""}
                  onChange={(e) => set("methods", e.target.value)} /></Field>
              <Field label="UA 包含" hint="逗号分隔">
                <input className="nl-set-input" value={draft.userAgents ?? ""}
                  onChange={(e) => set("userAgents", e.target.value)} /></Field>
              <Field label="JA3" hint="仅透明路径可算" wide>
                <input className="nl-set-input" value={draft.ja3 ?? ""}
                  onChange={(e) => set("ja3", e.target.value)} /></Field>
              <Field label="内容类型" hint="json,image,video,xml">
                <input className="nl-set-input" value={draft.contentTypes ?? ""}
                  onChange={(e) => set("contentTypes", e.target.value)} /></Field>
              <Field label="响应最小（B）">
                <input className="nl-set-input" type="number" value={draft.sizeMin ?? ""}
                  onChange={(e) => set("sizeMin", e.target.value)} /></Field>
              <Field label="响应最大（B）" hint="0＝不限">
                <input className="nl-set-input" type="number" value={draft.sizeMax ?? ""}
                  onChange={(e) => set("sizeMax", e.target.value)} /></Field>
            </div>
            <SaveBar label="保存记录范围" onClick={() => applySettings("scope", scopeValues())} />
          </Card>
        ) : null}

        {section === "subs" ? (
          <>
            <Card title="添加订阅" desc="粘贴 QuanX(.conf) / Surge(.conf) / 纯域名列表地址，引擎自动适配">
              {!draft.subEngine ? (
                <p className="nl-set-warn">引擎未运行，订阅功能不可用——请先开启抓包。</p>
              ) : null}
              <div className="nl-set-grid">
                <Field label="订阅地址" wide>
                  <input className="nl-set-input" placeholder="https://example.com/quanx.conf" value={subForm.url}
                    onChange={(e) => setSubForm({ ...subForm, url: e.target.value })} />
                </Field>
                <Field label="备注">
                  <input className="nl-set-input" placeholder="可选" value={subForm.name}
                    onChange={(e) => setSubForm({ ...subForm, name: e.target.value })} />
                </Field>
                <Field label="格式">
                  <select className="nl-set-input" value={subForm.format}
                    onChange={(e) => setSubForm({ ...subForm, format: e.target.value })}>
                    <option value="auto">自动识别</option>
                    <option value="quanx">QuanX</option>
                    <option value="surge">Surge</option>
                    <option value="list">纯列表</option>
                  </select>
                </Field>
                <Field label="更新间隔（分）" hint="0＝不自动更新">
                  <input className="nl-set-input" type="number" min={0} step={5} value={subForm.interval}
                    onChange={(e) => setSubForm({ ...subForm, interval: Number(e.target.value) })} />
                </Field>
              </div>
              <div className="nl-set-row">
                <Button size="sm" className="gap-1.5" disabled={!subForm.url || Boolean(draft.subBusy)}
                  onClick={() => {
                    applySettings("sub-add", { subUrl: subForm.url, subName: subForm.name,
                      subFormat: subForm.format, subInterval: subForm.interval })
                    setSubForm({ url: "", name: "", format: "auto", interval: 0 })
                  }}><Plus size={13} /> 添加订阅</Button>
                <Button size="sm" variant="outline" className="gap-1.5" onClick={() => settingsAction("subs-load")}>
                  <RefreshCw size={13} /> 刷新</Button>
              </div>
            </Card>
            {draft.subEffective ? (
              <Card title="当前生效" desc="启用订阅的并集">
                <div className="nl-set-effective">
                  <span>解密域名 <b>{arrLen(draft.subEffective.domains)}</b></span>
                  <span>引擎规则 <b>{num(draft.subEffective.rules)}</b></span>
                  <span>脚本 <b>{num(draft.subEffective.scripts)}</b></span>
                  <span>IP 段 <b>{arrLen(draft.subEffective.dst_ips)}</b></span>
                  <span>国家 <b>{arrLen(draft.subEffective.countries)}</b></span>
                </div>
              </Card>
            ) : null}
            <Card title="订阅列表" desc={`${(draft.subs ?? []).length} 条订阅`}>
              {(draft.subs ?? []).length === 0 ? (
                <span className="nl-set-empty">还没有订阅，粘贴地址后规则会自动适配到抓包范围与改写规则</span>
              ) : (draft.subs ?? []).map((s: SubEntry) => (
                <div className="nl-set-sub" key={s.id}>
                  <div className="nl-set-sub-head">
                    <b>{s.name}</b>
                    <Badge variant="outline" className="text-[10px]">{s.format}</Badge>
                    <Badge variant="outline" className="text-[10px]">{s.enabled ? "已启用" : "已暂停"}</Badge>
                    {s.interval_min ? <small>每 {s.interval_min} 分钟更新</small> : null}
                    <span className="nl-set-sub-actions">
                      <Button size="sm" variant="outline" className="h-7 gap-1 text-[11px]" disabled={Boolean(draft.subBusy)}
                        onClick={() => settingsAction("sub-update", { id: s.id })}><RefreshCw size={11} /> 更新</Button>
                      <Button size="sm" variant="outline" className="h-7 gap-1 text-[11px]" disabled={Boolean(draft.subBusy)}
                        onClick={() => settingsAction("sub-toggle", { id: s.id })}>
                        {s.enabled ? <Pause size={11} /> : <Play size={11} />}{s.enabled ? "暂停" : "启用"}</Button>
                      <Button size="sm" variant="outline" className="h-7 gap-1 text-[11px]"
                        onClick={() => settingsAction("sub-detail", { id: s.id })}><Eye size={11} /> 明细</Button>
                      <Button size="sm" variant="outline" className="h-7 gap-1 text-[11px] text-rose-600" disabled={Boolean(draft.subBusy)}
                        onClick={() => settingsAction("sub-del", { id: s.id })}><Trash2 size={11} /> 删除</Button>
                    </span>
                  </div>
                  <div className="nl-set-mono nl-set-sub-url">{s.url}</div>
                  <div className="nl-set-sub-meta">
                    域名 {s.host_count ?? 0} · IP {s.ip_count ?? 0} · 规则行 {s.rule_count ?? 0} ·
                    改写 {s.rewrite_count ?? 0} · 脚本 {s.script_count ?? 0}
                    {s.last_error ? <span className="nl-set-warn">错误：{s.last_error}</span> : null}
                  </div>
                  {draft.subDetail?.id === s.id ? (
                    <pre className="nl-set-detail">{JSON.stringify(draft.subDetail.parsed ?? {}, null, 2)}</pre>
                  ) : null}
                </div>
              ))}
            </Card>
          </>
        ) : null}
      </div>
    </div>
  )
}

function arrLen(v: unknown): number { return Array.isArray(v) ? v.length : 0 }
function num(v: unknown): number { return typeof v === "number" ? v : 0 }
