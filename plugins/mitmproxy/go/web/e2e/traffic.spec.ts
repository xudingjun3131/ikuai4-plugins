import { spawn, type ChildProcess } from "node:child_process"
import { createServer } from "node:http"
import { mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { test, expect } from "@playwright/test"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..")
const bin = path.join(root, "bin", "proxying")

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const s = createServer()
    s.listen(0, "127.0.0.1", () => {
      const addr = s.address()
      if (!addr || typeof addr === "string") {
        s.close()
        reject(new Error("no port"))
        return
      }
      const { port } = addr
      s.close((err) => (err ? reject(err) : resolve(port)))
    })
  })
}

async function waitHealth(apiBase: string, ms = 15_000): Promise<void> {
  const deadline = Date.now() + ms
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`${apiBase}/api/health`)
      if (r.ok) return
    } catch {
      // retry
    }
    await new Promise((r) => setTimeout(r, 50))
  }
  throw new Error(`health timeout: ${apiBase}`)
}

test.describe("traffic UI", () => {
  let proc: ChildProcess | null = null
  let dataDir = ""
  let apiBase = ""
  let proxyAddr = ""
  let origin: ReturnType<typeof createServer> | null = null
  let originPort = 0

  test.beforeAll(async () => {
    dataDir = await mkdtemp(path.join(tmpdir(), "proxying-e2e-"))
    const apiPort = await freePort()
    const proxyPort = await freePort()
    originPort = await freePort()
    apiBase = `http://127.0.0.1:${apiPort}`
    proxyAddr = `127.0.0.1:${proxyPort}`

    origin = createServer((_req, res) => {
      res.writeHead(200, { "Content-Type": "text/plain" })
      res.end("e2e-origin-ok")
    })
    await new Promise<void>((resolve) => {
      origin!.listen(originPort, "127.0.0.1", () => resolve())
    })

    proc = spawn(
      bin,
      [
        "-api-addr",
        `127.0.0.1:${apiPort}`,
        "-proxy-addr",
        proxyAddr,
        "-data-dir",
        dataDir,
      ],
      { stdio: ["ignore", "pipe", "pipe"] },
    )
    proc.stderr?.on("data", () => {})
    proc.stdout?.on("data", () => {})
    await waitHealth(apiBase)
  })

  test.afterAll(async () => {
    if (proc && !proc.killed) {
      proc.kill("SIGTERM")
      await new Promise<void>((resolve) => {
        proc!.once("exit", () => resolve())
        setTimeout(() => {
          try {
            proc!.kill("SIGKILL")
          } catch {
            // ignore
          }
          resolve()
        }, 2000)
      })
    }
    if (origin) {
      await new Promise<void>((resolve) => origin!.close(() => resolve()))
    }
    if (dataDir) {
      await rm(dataDir, { recursive: true, force: true })
    }
  })

  test("row appears after proxied flow; detail shows method; CA link present", async ({
    page,
  }) => {
    await page.goto(apiBase + "/")
    await expect(page.getByTestId("traffic-list")).toBeVisible()

    // Inject flow via real MITM proxy (no test-only POST API).
    const target = `http://127.0.0.1:${originPort}/e2e-path`
    const curl = spawn(
      "curl",
      [
        "-sS",
        "-o",
        "/dev/null",
        "-x",
        `http://${proxyAddr}`,
        "-X",
        "GET",
        target,
      ],
      { stdio: "ignore" },
    )
    await new Promise<void>((resolve, reject) => {
      curl.once("exit", (code) =>
        code === 0 ? resolve() : reject(new Error(`curl exit ${code}`)),
      )
    })

    const row = page.getByTestId("traffic-row").first()
    await expect(row).toBeVisible({ timeout: 10_000 })
    await expect(row.getByTestId("traffic-row-method")).toHaveText("GET")

    await row.click()
    await expect(page.getByTestId("traffic-detail")).toBeVisible()
    await expect(page.getByTestId("detail-method")).toHaveText("GET")

    const ca = page.getByTestId("download-ca")
    await expect(ca).toBeVisible()
    await expect(ca).toHaveAttribute("href", "/api/ca.pem")
  })
})
