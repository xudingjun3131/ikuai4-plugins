// [plugin-proxy] iKuai 加固: WebUI 经路由器同源 /plugin_proxy/runhooks/ 代理
// (OpenResty 校验 sess_key 后 StripPrefix 转发到 127.0.0.1:引擎端口)。
// Vite base 即该前缀, index.html 的资源引用已由 Vite 改写为带前缀的绝对路径;
// 所有 URL 现在在 lib/api.ts 的 apiURL() 生成时就带前缀, 包括 <a href> ——
// <a> 不走 fetch, 是此前真机 404 的根因。这里保留 fetch/EventSource 包装作为
// 兜底: 任何后续手写或新增的 /api/ 绝对路径调用仍会被自动补前缀。
// 引擎侧同时保留裸 /api/* 路由(插件 service.sh 本地调用用), 两者互不影响。
const BASE = (import.meta.env.BASE_URL || "/").replace(/\/+$/, "")

if (BASE) {
  const origFetch = window.fetch.bind(window)
  window.fetch = (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    if (typeof input === "string") {
      if (input.startsWith("/api/")) input = BASE + input
    } else if (input instanceof URL) {
      if (input.pathname.startsWith("/api/")) {
        input = new URL(BASE + input.pathname + input.search, input.origin)
      }
    } else if (input instanceof Request) {
      const u = new URL(input.url)
      if (u.pathname.startsWith("/api/")) {
        input = new Request(BASE + u.pathname + u.search, input)
      }
    }
    return origFetch(input, init)
  }

  // /api/events 是 SSE 长连接(use-flows.ts), 同样要带前缀
  const OrigES = window.EventSource
  class PrefixedEventSource extends OrigES {
    constructor(url: string | URL, config?: EventSourceInit) {
      super(
        typeof url === "string" && url.startsWith("/api/") ? BASE + url : url,
        config,
      )
    }
  }
  window.EventSource = PrefixedEventSource as typeof EventSource
}
