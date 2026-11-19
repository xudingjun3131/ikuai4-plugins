import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
// [plugin-proxy] 必须最先于 App 加载(包装 fetch/EventSource 给 /api/* 绝对路径
// 补 /plugin_proxy/runhooks 前缀, 配合 Vite base 的代理路径)
import "./lib/api-base"
import "./index.css"
import App from "./App.tsx"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
