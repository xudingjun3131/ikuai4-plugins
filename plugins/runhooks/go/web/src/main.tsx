import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
// [plugin-proxy] iKuai 加固: 必须最先于 App 加载(包装 fetch/EventSource 给
// /api/* 绝对路径补 /plugin_proxy/runhooks 前缀, 配合 Vite base 的代理路径)
import "./lib/api-base"
import "./index.css"
import App from "./App.tsx"

document.documentElement.classList.add("dark")

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
