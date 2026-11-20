// runhooks v1.1.8 —— 浏览器级补证: 可见浏览器经代理访问已改包页面, DOM 渲染出改写文案。
// 落实 v1.1.7 测试报告 §3.7「再否定/改进」: 把「实际客户端」从 curl 拓到真实浏览器。
//
// 用法:
//   node smoke_browser_dom_rewrite.mjs                 (headless)
//   xvfb-run -a env HEADFUL=1 node smoke_browser_dom_rewrite.mjs   (虚拟显示下的可见窗口)
// 产物: docs/EVIDENCE-2026-11-20-v1.1.8-browser-dom-rewrite.png
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import path from "node:path"
import fs from "node:fs"
import http from "node:http"
import { spawn } from "node:child_process"

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const goDir = path.resolve(__dirname, "..")
const pluginDir = path.resolve(goDir, "..")
const webDir = path.join(goDir, "web")
const repoRoot = path.resolve(pluginDir, "..", "..")
const require = createRequire(path.join(webDir, "package.json"))
const { chromium } = require("playwright")
const BIN = process.env.BIN || path.join(pluginDir, "bins", "runhooks-x86_64")
const EVIDENCE = path.join(repoRoot, "docs", "EVIDENCE-2026-11-20-v1.1.8-browser-dom-rewrite.png")
const HEADFUL = process.env.HEADFUL === "1"

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = http.createServer()
    srv.listen(0, "127.0.0.1", () => {
      const p = srv.address().port
      srv.close(() => resolve(p))
    })
    srv.on("error", reject)
  })
}

async function api(port, method, endpoint, body) {
  const res = await fetch(`http://127.0.0.1:${port}${endpoint}`, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${method} ${endpoint}: HTTP ${res.status} ${await res.text()}`)
  const text = await res.text()
  return text ? JSON.parse(text) : null
}

async function main() {
  if (!fs.existsSync(BIN)) throw new Error("engine binary missing: " + BIN)
  const tmp = fs.mkdtempSync(path.join(process.env.TMPDIR || "/tmp", ".qa-v118-browser-"))
  const data = path.join(tmp, "data")
  fs.mkdirSync(path.join(data, "scripts"), { recursive: true })

  const proxyPort = await freePort()
  const apiPort = await freePort()
  const upstream = http.createServer((_req, res) => {
    const html = "<!doctype html><html lang='zh'><head><meta charset='utf-8'><title>QA V118</title></head>" +
      "<body style='font:32px sans-serif;padding:40px'><h1 id='t'>BROWSER-ORIGINAL-BODY</h1></body></html>"
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8", "Content-Length": Buffer.byteLength(html) })
    res.end(html)
  })
  await new Promise((r) => upstream.listen(0, "127.0.0.1", r))
  const upPort = upstream.address().port

  const env = { ...process.env, RUNHOOKS_RECORD_SELF_TRAFFIC: "1" }
  const eng = spawn(BIN, [
    "-proxy-addr", `127.0.0.1:${proxyPort}`,
    "-api-addr", `127.0.0.1:${apiPort}`,
    "-data-dir", data, "-script-timeout", "100ms",
  ], { env, stdio: "ignore" })

  let browser
  try {
    let healthy = false
    for (let i = 0; i < 60; i++) {
      try {
        const h = await api(apiPort, "GET", "/api/health")
        if (h && h.ok) { healthy = true; break }
      } catch { /* not up yet */ }
      await sleep(200)
    }
    if (!healthy) throw new Error("engine not healthy")

    await api(apiPort, "POST", "/api/rules", {
      id: "qa-v118-browser-rewrite", enabled: true, type: "body_replace",
      match: { host: "127.0.0.1", path: "/browser" },
      body_replace: { phase: "response", from: "BROWSER-ORIGINAL-BODY", to: "BROWSER-DOM-REWRITTEN-V118" },
    })

    browser = await chromium.launch({
      headless: !HEADFUL,
      args: [
        "--no-sandbox",
        `--proxy-server=http://127.0.0.1:${proxyPort}`,
        "--proxy-bypass-list=<-loopback>",
        "--ignore-certificate-errors",
      ],
    })
    const page = await browser.newPage()
    await page.goto(`http://127.0.0.1:${upPort}/browser`, { waitUntil: "load", timeout: 20000 })
    const text = await page.locator("#t").textContent()
    const ok = (text || "").includes("BROWSER-DOM-REWRITTEN-V118")
    fs.mkdirSync(path.dirname(EVIDENCE), { recursive: true })
    await page.screenshot({ path: EVIDENCE, fullPage: true })
    console.log(`DOM_TEXT=${JSON.stringify(text)}`)
    console.log(`HEADFUL=${HEADFUL}`)
    console.log(`EVIDENCE=${EVIDENCE}`)
    console.log(ok ? "PASS  浏览器 DOM 渲染出改写文案" : "FAIL  浏览器 DOM 未渲染改写文案")
    if (!ok) process.exitCode = 1
  } finally {
    if (browser) await browser.close()
    eng.kill("SIGTERM")
    await new Promise((r) => upstream.close(r))
    fs.rmSync(tmp, { recursive: true, force: true })
  }
}

main().catch((e) => {
  console.error("FAIL  " + (e && e.stack ? e.stack : e))
  process.exitCode = 1
})
