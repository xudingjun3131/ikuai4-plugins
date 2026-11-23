# AGENTS.md — iKuai4 插件仓库适配与打包要点

本仓库（`/opt/ikuai4-plugins`）是 iKuai OS V4 企业版加固固件的插件源，设备侧软件源
`https://xudingjun3131.github.io/ikuai4-plugins`（gh-pages 承载 plugins.json / ipk / img.tar.gz）。
配套固件仓库 `/opt/ikuai4-enterprise`（插件框架 `iKuaiPatch`、构建/核验脚本、`AGENTS.md` 主文档），
完整坑位清单在 Hermes skill `ikuai-firmware-build`（`skill_view name=ikuai-firmware-build`）——
本文件只记**插件开发/适配/打包**最常踩的部分，改插件前必读。

---

## 1. 插件结构约定

```
plugins/<name>/
├── metadata.json      # 包元数据(构建时生成 ipk 内 html/metadata.json)
├── install.sh         # 安装/开机脚本, 必须 install ${1-boot}
├── logo.png           # 128px PNG, 缺了 build.py 静默产出半包(坑: 打到 logo 才报错)
├── html/              # 页面(index.html + 静态资源), iframe 嵌入 SPA
├── script/            # service.sh 及辅助脚本(部分插件把主逻辑放 script/<name>.sh)
└── bins/ 或 bin/      # 二进制与数据文件(见 metadata 的 bin/include 字段)
```

**install.sh 契约**（框架 `__install` 装完跑 `install.sh <type>`，boot() 每次开机对每个已装插件跑 `install.sh boot`）：

```bash
#!/bin/bash
BASH_SOURCE=$0
INSTALL_DIR="$(cd "$(dirname "$0")" && pwd)"          # 自定位, 别信 config.sh 的路径
PLUGIN_NAME="$(jq -r '.name' $INSTALL_DIR/html/metadata.json)"
chmod +x $INSTALL_DIR/script/* $INSTALL_DIR/bins/*    # 有 bins/ 的话
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null
install(){
    type=$1                                            # new/upgrade/reinstall/boot
    rm -rf /usr/ikuai/www/plugins/$PLUGIN_NAME
    ln -sf $INSTALL_DIR/html /usr/ikuai/www/plugins/$PLUGIN_NAME
    ln -sf $INSTALL_DIR/script/service.sh /usr/ikuai/function/plugin_$PLUGIN_NAME
    ln -sf ./install.sh $INSTALL_DIR/uninstall.sh
    [ "$type" = "new" -o "$type" = "reinstall" ] && mkdir -p $EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME
    if [ -f "$EXT_PLUGIN_CONFIG_DIR/$PLUGIN_NAME/autostart" ]; then
        /usr/ikuai/function/plugin_$PLUGIN_NAME start || true
    fi
    return 0                                           # 不能以 [ ] && cmd 收尾(坑 89)
}
install ${1-boot}
```

- **符号链接每次 install() 都重建**（含 boot 类型）——存储路径变更后开机自愈靠这个
- **开机自启标志**：`$EXT_PLUGIN_CONFIG_DIR/<name>/autostart`，页面 set_auto_start 写、install.sh boot 读
- **install()/start() 最后一条不能是 `[ ] && cmd`**：新装时 autostart 不存在 → `[ -f ]` 返回 1 → `&&` 短路 → install() 返回 1 → 框架判"插件安装失败"（但文件已就位，重进页面又显示成功）

**service.sh 契约**（经 `/usr/ikuai/function/plugin_<name>` 符号链接被 ikcommon 调用）：

```bash
#!/bin/bash /etc/ikcommon
PLUGIN_NAME="<name>"
. /usr/ikuai/script/plugin_env.sh 2>/dev/null || . /etc/mnt/plugins/configs/config.sh 2>/dev/null
```

- 动作函数名即 API action（`start`/`stop`/`show`/`set_xxx`），参数以 `key=value` 传入，函数内直接 `$key` 取值
- **start() 必须用 `start-stop-daemon -S -x <全路径/bin> -b -- <args> >/dev/null 2>&1`**：func 执行链是 `/Action/call` → `resty.shell.run()`(60s 超时) → `func_api.sh`，非 show 动作走 `Exec_Return` 的**命令替换** `$(...)`；裸 `daemon &` 会让守护进程继承管道写端 → 替换永远等不到 EOF → 60s 阻塞 → 进程下次写 stdout 吃 SIGPIPE 打死 → 端口从不监听。stdio 两个都要重定向；重定向目标必须存在（`>/dev/dell` 这类错别字直接让命令失败）
- **start() 加自检**：进程不在就 tail 日志并返回失败，不静默成功
- 页面**必须有 启动/停止 单按钮**（按状态合并：`v-if="status !== 1"` 显示启动、否则显示停止），pgstore 页面没有 start/stop，引擎只能由插件自己页面拉起

---

## 2. iKuai4 适配重要细节（踩过的坑）

### 2.1 二进制与加载器

- 固件用户态是 **uClibc**（`/lib/ld64-uClibc-1.0.40.so`），**没有 glibc loader**（`find <rootfs> -name 'ld-linux*'` 命中 0）。musl 链接的 OpenWrt 包跑不了；`go build` 默认 CGO_ENABLED=1 会链出 `PT_INTERP=/lib64/ld-linux-x86-64.so.2`——**即使纯 Go 也照样带**，必须 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w"` 静态编译（-s -w 把 25MB 压到 19MB）
- **交付前门禁**：任何要进 ipk 的二进制先查 PT_INTERP，非空就静态重建。症状是 exec 报 `No such file or directory`——这个"找不到"指的是**加载器**不是二进制，重装/升级/改存储路径全部无效
- mipsle 要先 `readelf -A <bin> | grep -i float` 核对软/硬浮点（iKuai 是 soft float，下错 hardfloat 变体设备上跑不起来）
- **企业版 x64 身份所有在线下载被服务端 400 拒绝**——插件二进制必须随 ipk 发货，不能指望设备侧下载

### 2.2 文件系统与路径（storage-anchor，2026-11-17 起）

- **rootfs 是 100% 满的 RAM 盘**（`root=/dev/ram0`，仅剩 ~6MB，`/tmp` 就在 rootfs 里且没有 tmpfs）：插件的工作目录/日志/下载暂存**一律不许写 /tmp 或 rootfs**。adguard 把 11MB 二进制解压到 /tmp 必 ENOSPC → 端口从不监听；npc 日志写 /tmp/npc.log 涨爆 RAM
- **环境变量唯一来源是框架 helper `/usr/ikuai/script/plugin_env.sh`**（source 它；旧固件回退 `/etc/mnt/plugins/configs/config.sh`）。它导出的变量：
  - `EXT_PLUGIN_INSTALL_DIR` 安装目录（`<存储卷>/plugins_installed`，未设存储时 `/etc/log/plugins_installed`）
  - `EXT_PLUGIN_CONFIG_DIR` 插件配置目录（`<卷>/plugins_configs`，未设存储 `/etc/log/plugins_configs`）
  - `EXT_PLUGIN_LOG_DIR` 日志目录（`/etc/log/plugins/logs`，固定 p5）
  - `EXT_PLUGIN_IPK_DIR` ipk 目录、`EXT_PLUGIN_FW_DIR` 框架数据目录（config.sh/repos/img）
  - `EXT_PLUGIN_STORAGE_SET/PATH/DEV`、`EXT_PLUGIN_RUN_INMEM`
- **配置分区 sda3（/etc/mnt）只有 43MB，不存放任何插件安装/配置数据**（用户硬性要求）。存储卷没挂上时 `/etc/disk_user/<卷名>` 这类目录还在 sda3 上——helper 的 `__plugin_env_fs_ok` 会拒绝落在配置分区/根文件系统的路径；插件侧**不要自己判断路径，一律跟随变量**
- 大数据（GEOIP CSV、引擎 data-dir、querylog）放**安装目录同级 `.xxx`**（`upgrade_online` 会整体替换插件安装目录，放里面的缓存每次升级被冲掉；同级不受影响）；引擎只认自己 dataDir 里的文件时用 `ln -sf` 把大分区文件链进去（Go os.Open 跟随符号link）
- 迁移要做成"新位置就位后才删旧目录"；用户数据（CA 证书、设备 UUID）用 mv/cp 保留——删了重建要全部客户端重装/重新绑定
- busybox `df` 解析必须 `df -hP`/`df -kP` + `awk 'END{print $N}'`（busybox df 对长文件系统名会折行；列序 blocks used avail use% mount，$2 是 used 不是 total）

### 2.3 func 分发与 /Action/call 协议

- 链路：`/Action/call` → `webman.lua` → `ikngx.rest()`(60s 超时) → `func_api.sh` → `/etc/ikcommon` 的 `Command()`（source 脚本 + `local $@` 把 `key=value` 变局部变量 + 调动作函数）
- **只有 `show` 动作能把 stdout 带回前端**（`Show()` 包成 `{"code":0,"results":<stdout>}`）；**其余全部动作走 `Exec_Return`，成功（退出码 0）即丢弃 stdout**。所以：诊断/回显类功能必须写成 `__show_xxx()` + 前端 `action:'show', param:{TYPE:'xxx'}`（`Show()` 按 `${TYPE//,/ }` 分发 `__show_$TYPE`）；独立 action 里 echo 再多也回不来
- **动作名校验**：函数定义行必须匹配 `^[A-Za-z][A-Za-z0-9._@%]+" "\(\)`（零或多个空格），**下划线开头的函数是内部函数不可 API 直调**（`__show_*` 正因此只能经 Show 分发）
- **参数禁特殊字符**：`ikngx.lua` 的 `__rest_check_illegality_char` 禁 `' " ` $ & ; \ | 回车 空格`（TAB 在 315 静默切分、317 直接报错）。页面传 JSON 要 base64：参数名加 `_b64` 后缀，值用 `btoa(unescape(encodeURIComponent(s)))`；后端 `${!1}` base64 -d 解，无 `_b64` 时回退明文
- **响应字段映射**：4.x `/Action/call` 返回 `code/results/message`，而 3.7x 时代页面读 `Result/Data/ErrMsg`——统一在 `plugin.js` 的 AjaxPost onload 里映射（所有插件页面都加载 plugin.js，一处修全部；各插件自己的 index.html 也走这条）
- 错误提示：后端 `outputErrorLog` 产 `{code,message,errors,details}`，前端把 message+errors 用逗号连接显示

### 2.4 busybox 用户态

- **无 od/file/strings/tr/cmp**：二进制检查用 bash 变量+模式匹配（`magic=$(head -c 4 "$f")` + `case`），判 loader 用 python（构建机侧）
- `jq` 在 `/usr/sbin`；**`/etc/ikcommon` 第 5 行把 PATH 重置为 `/usr/bin:/bin:/usr/sbin:/sbin`**（不含 /usr/local/bin）——测试脚本放 /usr/local/bin 的影子替换完全不被调用
- busybox `test -nt` 不支持，"比源文件新才重解压"用标志文件或 size 判断
- `awk 'while(getline<文件)'` 在文件缺失时是 **100% CPU 死循环**（返回 -1 恒真）——全部改 `>0` 判断
- 杀测试进程用 `pgrep -f 'xxx[y]'` 括号技巧或先拿 PID 再 kill；`pkill -f '<模式>'` 会命中自己所在的命令行把自己杀掉

### 2.5 前端页面（iframe 嵌入 SPA）

- 插件页是 iframe（`/pluginStore` → `<iframe src="/plugins/<name>/index.html">`），**官方 `.setting` 的主题变量不跨 iframe**——页面必须自带背景体系（`body{background:#e9edf4}` + 白卡 8px 圆角描边阴影），引 `var(--Block_Background)` 解析为空
- **布局对齐官方 SPA 滚动语义**：`html,body{height:100%;overflow:hidden}` + `#app{height:100%;padding:16px;overflow:hidden;display:flex;flex-direction:column}` + `#app>.box{flex:1 1 0;min-height:0;overflow-y:auto;padding-left:0;padding-right:0}` + `.plugin_mgmt{min-height:0}`。16px 是官方实测值；上下留白 `#app` 8px + `.box` margin 10px = 18px（v3 灰条砍半结论，v1/v2 已废弃）
- **element.js 是部分打包，`el-tabs`/`el-tab-pane` 没注册**——多段筛选用纯 HTML+Vue 自实现 tab（flex 按钮栏 + `:class` + `v-show`），只有页面原本在用的 Element 组件才可依赖
- **02.pgstore 这个 Vue 页面对复杂响应式表达式不渲染**（`v-if` 链/`:style` 对象/`{{ method() }}` 全不渲染，加 `<tbody>` 甚至让整个 `v-else` 表格失效）——新 UI 反馈一律用原生 DOM（静态 span + AjaxPost 回调里 textContent）
- 指向"引擎未启动时必然连不上"的 iframe/链接必须门控隐藏（包进 `<template v-if="status === 1">`，含上下间距 div）
- 版面偏好：多段条件筛选一律 tab 化；已选项必须可见（tab 标签带计数 + 标签云可移除）；不要功能重复的按钮对（"添加并保存"合并）；指向"引擎未启动时必然连不上"的 iframe/链接必须门控隐藏
- 验证必须 chromium 实测（mock 服务 + 真实渲染），只看文件在不在或计算样式全对都没用

### 2.6 框架侧联动（改插件会碰到的）

- **插件管理页（02.pgstore）只列 iKuai 文件管理的卷名**（`/etc/disk_user/*`，UI 路径 `/` 即 `/etc/disk_user`）——用户要求"不能把系统存储带出来，只能是磁盘管理→文件管理里定义的名称"
- **每次改插件必须升 metadata.json 的 version**（patch +1）：pgstore 的 `compare_version` 是严格语义化比较（`v1*10000+v2*100+v3`），newversion 不大于 oldversion 就不注入升级字段、页面不渲染升级按钮。**先读当前版本再往上加**，写成比线上旧版更低的号比不升更糟
- 升级只换二进制不重启守护进程=没生效：凡改动落在二进制或二进制内嵌资源（Go 引擎内嵌 JS/HTML/证书）的插件，升级后必须 stop+start。验收顺序：upgrade_online → stop → start → 再拉一次设备端实际发出的资源逐字节核对
- `upgrade_online` 整体替换插件安装目录，但跑着的进程仍持旧代码；`stop()` 只杀 PIDFILE 里的 PID，杀不掉就直接重试 start（端口会自行释放，别判"二进制坏了"）
- 在线安装下载落 `/etc/log/plugins/import/file`（p5）；页面上传落 `/tmp/iktmp/import/file`（rootfs RAM，>6MB 必失败）——大插件引导用户走在线安装
- `__install` 用 `tar -xzf ... 2>/dev/null` 把解压错误完全静默：磁盘不足时小文件成功、末尾大文件失败，安装却显示成功。install.sh 装完自检 `bins/*` 非空，service.sh exec 前检查存在性与可执行权限，把"磁盘空间不足"和 `df` 输出直接摆给用户
- 设备 SSH 23/22 全关时，远程诊断走 Web UI（SDK `scripts/ikuai4-sdk`，登录 `pass=Base64("salt_11"+pass)`；诊断输出必须走 show(TYPE)）

---

## 3. 打包流程

### 3.1 本地构建

```bash
# 全量: 第一个参数是插件父目录(每个子目录当一个插件), 不是插件目录本身
python3 build.py plugins output
# 单插件(传 plugins/reqable 会把 html/script/bins 当成三个插件, 必错):
mkdir -p /tmp/oneplug && cp -a plugins/reqable /tmp/oneplug/ && python3 build.py /tmp/oneplug /tmp/plugbuild
```

产物在 `<output_dir>/`：`ipk/plugin-<name>[-<arch>]-v<ver>-Build<ts>.ipk`、`plugins.json`（**只含本次构建的插件**，别直接覆盖线上那份）、`img.tar.gz`。

**ipk 内清单**（缺一不可，构建后必 `tar -tzf` 核对）：`html/index.html`、`html/logo.png`、`html/metadata.json`、`script/`、`install.sh`、`bins/`（或按 metadata `bin` 映射的 `bin/`）、`include` 列表里的文件。

**metadata.json 字段**：`name`（目录名即插件名）、`alias`（页面中文名）、`manufacturer`、`version`、`type`（external/internal）、`description`、`releasenotes`、`arch`（`["x86_64"]`/`["arm64"]`/`["mipsle"]`/`["all"]`）、`bin`（arch → 文件映射）、`include`（额外打包路径）、`compatibility`、`upgradetype`。

> ⚠️ **`name` 是设备侧主键，不是产品名，禁止因「品牌改名 / 大小写规范化」改动**：必须与 `plugins/<目录名>/` 及**设备已装版本**的字面值逐字节一致（大小写敏感）。改它等于切断设备升级链（已装项匹配不到在线清单 → 无 `newversion`、无升级按钮），详见文末「插件标识大小写」章节。改中文名/品牌只动 `alias`、`description`、WebUI 标题。

**改插件的标准动作**：改 `plugins/<name>/` 源码 → metadata.json version +1（assert 新号 > 旧号）→ 本地 build.py 验证打包 → 提交推送（CI 发布）→ 设备 updateSoftwareSouce → 升级。

### 3.2 发布（CI → GitHub Pages）

`.github/workflows/build-and-deploy.yml`：push 到 `main` 且 paths 命中 `plugins/**` 或 `build.py` 时触发——`python build.py plugins build` 构建**全部**插件 → 拉 GEOIP 数据（jsdelivr 镜像，设备从同一 CDN 取必然可达）→ `peaceiris/actions-gh-pages` 把 `build/` 推到 `gh-pages` → Pages 站点提供 `plugins.json` + `ipk/` + `geoip/`。**全程不经过 GitHub Release**。

**验收链路四步**（全过才算真发布）：push → 轮询 `actions/runs` 到 `completed|success` → 轮询线上 `plugins.json` 确认 version/build 已变 → **下载线上那个 .ipk 解出改动文件 grep 关键行**。别信版本号——页面显示的版本来自 plugins.json，与包内实际内容可能不一致。

**发布成功但设备看不到新版本时的排查顺序**：① Pages 有 `cache-control: max-age=600`（10 分钟 CDN 缓存），等 10 分钟或先点一次「更新软件源」；② 设备 `updateSoftwareSouce` 是 `wget -qO-` 实时拉、不读本地 HTTP 缓存，URL 没错时点一次就该生效；③ **URL 没错才是前提**——设备 repos.json 跨固件版本残留（固件升级不刷 p3），先看「软件源管理」里那条源的 URL 是不是 `https://xudingjun3131.github.io/ikuai4-plugins`；④ 别把"设备没显示"轻易归因成 CDN 缓存——先证实发布链路四步

### 3.3 纪律

- **推送由用户执行**（仓库由其自行维护，CI 推送会触发全量重建发布）；改完先告知，本地 commit 不 push
- 本地构建会弄脏工作区：`output/img.tar.gz` 是被 git 跟踪的——提交前必须 `git checkout -- output/img.tar.gz` 并删掉 `output/ipk/`、`output/plugins.json`，只提交 `plugins/<name>/` 下的源码文件
- 本地 .ipk 只用于验内容：设备走在线安装，>6MB 的包不能走页面上传（rootfs RAM）
- 用户报"我装的是最新版还是坏的"时**不要信版本号，直接下载线上那个 .ipk 验内容**：先从线上 plugins.json 读出 version/build，再 `curl -s <repo>/ipk/plugin-<name>-<arch>-v<ver>-Build<build>.ipk -o old.ipk && tar -xzOf old.ipk ./install.sh | grep -n '<关键行>'`

---

## 3.4 规则面板与脚本模板（2026-11-17 重做）

**规则面板（web/src/components/rules-panel.tsx）已从"只有 host_block"扩成完整编辑器**：12 种规则类型全部可页面增/改/停用/删——类型下拉 → 按类型动态字段表单（内容替换的查找/替换/阶段、头改写的键值对行编辑器、本地映射的状态码+文件、镜像远端 URL、上游网关、二级代理、反向代理监听+目标、访问控制 CIDR+动作、高亮颜色+标签）→ 匹配条件（域名 `*` 通配 / 路径**正则** / 方法逗号分隔）→ 保存走现有 `POST/PUT/DELETE /api/rules`（后端零改动，Save 原子写 rules.json + 热重载即时生效）。**最大坑：压缩响应**——`internal/api/display.go` 只在**显示层**解压 gzip/br/zstd，hook 层（rules/script/breakpoint）拿到的是**原始压缩字节**，明文替换匹配不上；解法是先加一条 request 阶段 header 规则把 `Accept-Encoding` 改 `identity`。

**脚本模板（internal/script/templates.go）**：goja 沙箱只注入 `flow` 对象——`flow.request.{method,url,headers,body}` / `flow.response.{status,headers,body}`，脚本定义 `function onRequest(flow){}` / `function onResponse(flow){}`，**直接改 flow 对象即生效，没有 `$done()`**；runner 对流**没有匹配过滤**，模板必须自查 URL（守卫）。QuantumultX-Rewrite（github.com/deezertidal/QuantumultX-Rewrite，277 个 rewrite conf）转换入库 10 个：Cubox/计算器HD/WallCraft/WPS/AudioMack/BedtimeFan/Emby/DayOne/Grammarly/TOKCAM。**QX→reqable 转换规则**：`$response.body`→`flow.response.body`、`$done({body})`→直接赋值、conf 里的 `^url` 匹配式→模板开头的 URL 守卫（**JS 正则字面量里的 `/` 必须转义**，否则提前终结正则）；带 `$tool`/`persistentStore`/`$prefs`/`$notify`/`require(`/sojson 混淆的脚本不可转（沙箱没有这些 API）。转换产物必须过两道验证：node 模拟沙箱跑"命中改写+未命中原样"（`/tmp/qxjs/test_templates.js` 模式）+ Go 回归测试 `Test_BundledTemplates_allValidGoja`/`Test_QXTemplates_guardBlocksNonMatching`（runner 真实执行守卫生效）。**顺修：原 3 个模板（add-header/strip-cookie/log-url）是顶层代码没包函数——runner 的 `AssertFunction(onResponse)` 取不到函数，从来就没生效过**，已全部包成函数。

### 3.5 内置规则（2026-11-17，v1.0.37）

`internal/rules/defaults.go` 的 `DefaultRules()` 返回 10 条内置规则：**请求头**禁用缓存（`Cache-Control/Pragma: no-cache` + 删 `If-None-Match`/`If-Modified-Since`，抓包不吃 304）、`Accept-Encoding: identity`、**响应头**放宽 CORS、移除 CSP/X-Frame-Options、**高亮** API（`^/api/` 蓝）/遥测（clarity、google-analytics、googletagmanager、doubleclick 红）/静态资源（灰）。**种子时机**：`NewChain` 发现 rules.json **不存在**才写入（全新 data-dir 首次启动自动有规则）；文件已存在**不覆盖**——用户删空的规则不会被开机复活；想随时找回来用规则面板「内置」按钮或 `PUT /api/rules`（整包替换）。内置规则 ID 一律 `builtin-` 前缀（面板打角标、`IsBuiltinID` 判定）。

**最大的坑：goproxy 会删 Accept-Encoding**——`RemoveProxyHeaders` 在转发前 `r.Header.Del("Accept-Encoding")`（除非 `KeepAcceptEncoding`），删完 Go `http.Transport` 发现没有该头又自动补 `gzip`，规则里设的 `identity` 被这俩联手干掉，上游永远回压缩流，「内容替换」的明文永远匹配不上。修法：`e.px.KeepAcceptEncoding = true`（没规则时客户端原始值直传，行为与之前等价；有规则时 identity 才生效）。**判据**：上游回显收到的请求头，`Accept-Encoding` 必须是 `identity` 而不是 `gzip`。

### 3.6 抓包总开关必须同时管住引擎记录层（2026-11-17，v1.0.38）

用户实踩：页面点「关闭抓包」后 proxy 还在抓、WebUI 里流量还在涨。**根因**：router 页的 `set_capture` 只撤了 iptables 透明转发规则（`iptables_del`），**引擎的记录层根本没停**——手动设了显式代理（18080）的设备流量照样进引擎照样落库，而 iptables 规则对显式代理流量毫无约束力。引擎自带的暂停能力（`PUT /api/capture {paused:true}`，store 的 `Add` 在 paused 时直接返回不存）router 页从来没调用过。**修法**：`__engine_capture()` 用 GNU wget（`/usr/bin/wget-ssl`，支持 `--method=PUT --body-data`；**不是 busybox wget**，别用 `--post-data`）调引擎 API，`set_capture` 两个分支都调；`start()` 启动后按 `$CAPTURE_FLAG` 有无对齐暂停态（引擎每次启动 paused 默认 false，不对齐的话"关了抓包再重启引擎"会静默恢复记录）。**页面三态**：`__show_data` 增加 `enginePaused`（GET /api/capture 的 `.paused`），标签显示 抓包中/已暂停(引擎)/抓包已关——总开关开着但引擎暂停时以前也显示"抓包中"，用户看着"在抓"却一条不记，和"关了还在抓"是同一类状态不透明。

### 3.7 断点全匹配失效 + 测试用例文档（2026-11-17，v1.0.39）

写测试文档时实测发现的真 bug：**断点拦截 `match.host:"*"`（全匹配）从来挂不住任何请求**——`internal/breakpoint/manager.go` 的 `hostMatch` 漏了 `*`/空 的特判，`*` 一路落到末尾 `strings.Contains(host, "*")`，host 里不可能含星号 → 恒 false → shouldHold 永假。`internal/rules/rule.go` 的同名函数一直有这条特判，breakpoint 包漏抄了。**判据**：同一种"匹配语义"在两个包里各有一份实现时，逐个对齐特判（`*`/空/`*.` 前缀）。另：断点开启后**一个流会先后挂两次**（请求阶段+响应阶段，match 不区分阶段），**每个 session 都要单独 resume**，客户端只有响应阶段放行后才收到内容——测试时只放请求阶段会看到"客户端拿到空响应"，不是 bug。

`docs/reqable-testcases.md`：18 条用例（TC-01~TC-18）覆盖引擎启停/抓包总开关真停/开关与启动对齐/三态显示/规则编辑器 12 类型/内置规则/改包再转发/压缩边界/QX 模板/原 3 模板/断点/设备选择/scope/CA/端口/数据目录 sda3/定时/暂停清空 HAR，附「已知边界（不是缺陷）」7 条与签收表。写测试文档本身就是最好的自查——本条 bug 就是写 TC-11 时实测发现的。

## 3.8 mitmproxy 插件（2026-11-17，v1.0.0）——自带 musl Python 运行时

**背景**：用户要把 mitmproxy（Python 应用）做成插件。**硬约束**：固件用户态是 uClibc，**没有 Python、没有 glibc loader、没有包管理器**（`find rootfs -name 'ld-linux*'` 命中 0，只有 `/lib/ld64-uClibc.so.0`）——PyPI wheel 直接扔上去跑不了；PyInstaller 产物链 glibc 同样起不来。

**解法（已验证）**：**自带完整 Alpine(musl) 运行时**。`apk add mitmproxy` 装进 Alpine minirootfs（Python 3.14 + mitmproxy 11 + 全部依赖），剪枝后约 100MB，整棵树进 ipk 的 `bins/runtime/`。启动用**自带的 musl 动态链接器**（`lib/ld-musl-x86_64.so.1`，static-pie、不依赖宿主任何 libc）：

```bash
exec "$B/lib/ld-musl-x86_64.so.1" --library-path "$B/lib:$B/usr/lib" \
    "$B/usr/bin/python3.14" "$B/usr/bin/mitmweb" "$@"
```

内核只认 PT_INTERP 指向的文件存在即可，显式调 loader 则完全绕开固件 libc——**与 uClibc/glibc 宿主无关**（原型在 glibc 宿主上验证：mitmweb HTTP 200 + 过代理抓到流 + 客户端收到断点改写的 body）。内存占用 RSS ~74MB。

**插件结构**（与 reqable 同构）：`script/service.sh`（start/stop/set_ports/set_auto_start/clear_flows/show&TYPE=data|ca）、`html/index.html`（状态/启停/端口/CA 同源 base64 下载/内嵌 mitmweb iframe）、`metadata.json` 用 `include:["bins/runtime"]` 打包目录树。**端口**：代理 18070 / Web 18071（页面可配，改了自动重启）。**confdir**（CA/配置/pid）落 `$EXT_PLUGIN_CONFIG_DIR/mitmproxy`——存储卷，**不落 sda3**（用户硬性要求）。

**坑**：① bundle 里的**悬空符号链接会让 build.py 崩**（`etc/mtab`→/proc/mounts、`etc/ssl1.1/cert.pem`）——打包前必须 `find . -xtype l` 清掉，否则 ipk 静默只打一半（31MB vs 102MB）；② mitmweb 有 **DNS rebind 保护**，但 LAN IP 访问放行（正则匹配 `[0-9.]+`）；③ `mitmweb` 是 console script 不是模块，入口用 `$B/usr/bin/mitmweb` 而非 `-m`；④ **ldap3/werkzeug 是硬依赖**（proxyauth addon import ldap3、werkzeug.security.safe_join），剪枝时删了会起不来；⑤ ipk 31MB >6MB，**只能在线安装**（页面上传必失败）；⑥ 启动要几秒（Python），start() 轮询 web 端口最多 15s；⑦ mitmweb API：`GET /flows`、`POST /clear`；CA 文件是 `confdir/mitmproxy-ca-cert.pem`（另有一对 `mitmproxy-ca.pem` 私钥，**严禁外发**）。

**build.py 符号链接坑（2026-11-17 真机安装时踩实，已修）**：`add_file`/`add_dir`/`get_default_permission` 都会 `open()` 源路径，**open 会跟随符号链接**。Alpine bundle 里有 330 个**绝对路径**链接（busybox applet 链接，全是 `/bin/busybox` 这种）——打包机上恰好有 `/bin/busybox` 时构建成功，换台没有的机器（GitHub CI 的 ubuntu-latest）直接 FileNotFoundError 整个构建崩。**本地绿 CI 红就是这么来的**（一度误判成 CI 超时/gh-pages 体积超限）。修法：`add_file` 把 `info.islnk()/issym()` 判断**早于** `get_default_permission`（它也要 open 读 shebang，是第二个崩点）并直接 return；`add_dir` 把目录符号链接挪进 files（os.walk 默认不跟随，链接目录原来被静默整个丢掉）；bundle 侧同步删掉全部绝对/逃逸链接（库链接全是相对的，删了不影响 .so 加载）。**教训：凡打包第三方 rootfs，先清绝对符号链接再提交。**

**真机验证（设备 192.168.9.1，2026-11-17，全部通过）**：在线安装 12s（31MB）→ start 3.3s → status=1/18070/18071/caok=1。过设备代理实测：`ifconfig.me/ip` 回显 **112.64.53.228（设备自己的公网 IP，证明流量真从设备出去）**、百度 200、**HTTPS 200（CONNECT+TLS 拦截通）**；mitmweb `/flows` 抓到 10 条；`show TYPE=ca` 返回 1564 字符 base64 PEM（字段名 **`ca_b64`**）；插件页 `GET /plugins/mitmproxy/` 200。

**真机安装的两个坑**：① `install_online` **必须传 `repo_id`**（默认仓库 id 固件种死 `5f74366d-090d-4540-a04f-d1d885f0b93f`），不传报"未找到仓库:"直接返回——SDK 裸调极易漏；② 设备侧 `www/plugins/<插件>/html/metadata.json` 若是 **0 字节**（升级 `cp -rf` 被打断的残留），`__show_data` 的 `cat` 对空文件也返回 0 → installed 数组多一个裸逗号 → **整个插件管理页 JSON.parse 失败**。现场急救：`install_online` 重装该插件（`rm -rf`+重拷即修复，reqable 实测 4.6s）；根治靠 Build202611171400 的 `[ -n "$_json" ]` 守卫。

**reqable 1.0.40/1.0.41 两个连在一起的升级链路 bug（测试 agent 验证时踩实，已修并上线）**：① **[stale-proc] 重装/升级插件不会重启引擎**——框架把新二进制拷到磁盘，但旧进程还在跑旧 inode（Linux 删运行中文件不影响已加载进程），`start()` 见到活 PID 直接 return 0 → 新功能要到重启才生效。用户/测试视角是"升级成功了但功能没变化"（实测：1.0.39 的断点全匹配修复装完后设备上仍不生效，一度被记成 FAIL）。修法：install.sh 对 new/upgrade/reinstall 三类安装先探活（`show TYPE=data | grep '"status":1'`），在跑才 stop+start，不在跑保持停止尊重用户状态；② **[stop-wait] stop() 发完 SIGTERM 立即返回**，进程退出和端口释放是异步的，紧接着 start 会 bind 失败（"引擎启动失败"，设备实踩竞态）→ 轮询等到进程真消失（≤5s）再 kill -9 兜底；③ **[ports-reread] 端口变量是脚本加载时 source 进内存的**，`set_proxy_ports` 写完 ports.conf 不重读，下面 start 用的还是旧端口（引擎重启了但还听老端口，用户视角"改端口要手动点一次启动才生效"）→ 写完立即 `. $PORTS_CONF`。**判据：凡"改启动参数"的 action，写完配置必须重读再重启；凡"升级"路径，必须处理在跑的旧进程。**

**reqable 1.0.42→1.0.43 静态链接回归（2026-11-18 设备实踩"引擎启动失败: No such file or directory"，用户反馈"这个不是修复了好几版吗"）**：v1.0.42 为修 WebUI fd 耗尽（IdleTimeout）重建引擎时用了裸 `go build`——**/opt/reqable 的 Makefile 三个构建目标全都没有 CGO_ENABLED=0**，产出 glibc 动态二进制（PT_INTERP=`/lib64/ld-linux-x86-64.so.2`）。固件只有 uClibc loader，设备 exec 时内核找不到 loader 返回 ENOENT，bash 把它报成二进制不存在——**坑 95 修过的问题被一次常规重建打回原形**（设备行号 281 与线上包 service.sh 吻合证明跑的就是最新脚本，坑 97 判据）。三道防线：① Makefile 构建目标写死 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` + `-s -w` 并附 static-gate 注释；② **build.py 加 `check_elf_interp` 硬门禁**（add_file 里查 64 位 ELF 的 PT_INTERP，含 `ld-linux` 直接中止构建并附修法；uClibc/musl 放行——mds/pppoe-scan 的 uClibc 动态链接与 mitmproxy 自带 musl runtime 不能误伤）；③ 引擎每次重建后冒烟六步（起引擎→端口监听→/api/flows 200→/ 200 且引用 assets JS→JS 响应体 grep 汉化关键词）不能省。**判据重申：设备报 No such file or directory 且行号与线上包吻合时，先解包线上 ipk 查包内二进制 PT_INTERP，不要只看仓库里的**（坑 93 同族：验发布产物不验本地）。

**mitmproxy v1.0.5→v1.0.6 启动误报连带界面变英文（2026-11-18 设备实锤）**：用户报"启动失败: mitmweb 启动异常(等了 15 秒 Web 端口没起来)，日志尾部 OpenSSL 3 legacy provider failed to load"，随后引擎实际又自己起来了，但 mitmweb 界面全是英文。根因链：mitmweb（Alpine Python 运行时）**冷启动超 15 秒** → 健康检查误报失败直接 return → 而 `__sync_web_lang` 写在成功分支里**从未执行** → 升级换 bundle 后设备上 lang.json（运行时文件）丢失 → i18n-zh.js `fetch /static/lang.json` 404 即**静默保持英文**。判据（一条 curl 定位）：index.html 有引 i18n-zh.js 且 `/static/i18n-zh.js` 返 200、仅 `/static/lang.json` 404 = 新 bundle 已装、缺的只是 lang.json（与坑 122 的"index.html 无引用=旧进程"是不同形态，注意区分）。修法三件：① **默认 `{"lang":"zh"}` 的 lang.json 随 bundle 发货**（`bins/runtime/.../web/static/lang.json` 进 ipk），升级后不依赖同步就是中文；② `__sync_web_lang` 移到启动引擎之前（只写磁盘，不需要引擎在场）；③ 健康检查 15s→60s + 失败日志 `grep -v 'legacy provider failed to load'` 过滤 Alpine 固有无害警告。**通用教训：健康检查的失败路径不能跳过任何"落盘型"收尾动作——误报+跳过=用户可见的功能缺失；等待预算按最坏冷启动估。**

## 4. 当前状态（2026-11-17）

- **28 个插件**：adguard clash ddnsgo frpc frps gecoos linkease lucky mds **mitmproxy** npc nps openp2p pppoe-scan reqable smartdns socat socks5 socks5-extapi syslog tailscale uu vidblock vlmcsd vnt vnts vremote zerotier（alist 已应用户要求下线）
- **两个自研/重型插件**：reqable（小黄鸟，Go 全静态引擎，当前 **1.0.43**，源码 `/opt/reqable`）+ **mitmproxy（1.0.6，自带 Alpine musl Python 运行时 102MB，见 3.8 节）**；设备实测双代理并行工作（reqable 18080/18081/18082，mitmproxy 18070/18071）
- **全部已完成 storage-anchor 迁移**：54 处 `source /etc/mnt/plugins/configs/config.sh` 改为框架 helper（旧固件回退），version 全 +1（adguard 2.1.7 … zerotier 1.2.2）；需搭配固件 `Build202611171200` 及以上（boot() 会自愈旧版插件的硬编码 source 行，不升级插件也能用）
- **15 个插件没有二进制**（tailscale/zerotier/openp2p/socat/linkease/vlmcsd/uu/vidblock/vremote/mds/pppoe-scan/syslog/socks5-extapi/gecoos/reqable 中部分为未完成的移植桩）——`service.sh` 引用的 `$plugin_dir/../bin/xxx` 在 ipk 里不存在，是移植桩；reqable 是自研引擎（源码 `/opt/reqable`，Go 全静态 + 汉化）
- **验证报告**：`docs/小黄鸟插件验证报告.md`（18 条用例，复验后 PASS 17 / FAIL 0 / 受限 1）；`docs/reqable-testcases.md` 为用例清单
- 固件侧插件框架的完整改动记录在 `/opt/ikuai4-enterprise/AGENTS.md`，坑位在 skill `ikuai-firmware-build`

## 插件 WebUI 零鉴权隐患修复（2026-11-18，reqable v1.0.44 / mitmproxy v1.0.7 / clash v2.2.3）

**隐患**：插件引擎 webui 原来直接绑 `0.0.0.0`，局域网内无痕窗口开 IP:端口即可看全部抓包内容/改设置——零鉴权。clash 的 ttyd（2222）更严重：**没有鉴权的 root shell 菜单**。

**修法（需固件 Build202611181800+，三插件同套）**：固件 OpenResty 加同源代理 `/plugin_proxy/<name>/`（Lua 校验 iKuai 登录会话 `sess_key` cookie 后转发到 `127.0.0.1`，无会话 GET 跳登录页/POST 返会话过期；端口文件协议 `/tmp/iktmp/plugin_web/<name>` 由插件启动写/停止删）；引擎 webui 只绑 `127.0.0.1`（直连已拒）；页面 iframe/window.open 改同源相对路径（自动带 cookie，顺带修复 HTTPS 管理页 mixed-content 拦 iframe）；service.sh 按 `BUILD_DATE` 自适应（旧固件回退直连，升级后自动收紧），show 回 `proxyReady` 标志。

**各插件适配要点**：
- reqable：Go 引擎双挂载（裸路径+前缀路径 StripPrefix）+ `X-Plugin-Proxy` 头防根路径 Location 回环；前端 Vite base + fetch/EventSource 全局包装补 `/api/` 前缀（覆盖 25 个调用点）；附带去顶部"开启抓包"按钮（与面板"继续"同功能）
- mitmproxy：app.js 17 处 fetchApi 绝对路径改相对（**WebSocket 用 `location.pathname` 构造天然兼容，不用改**）；i18n-zh.js 的 `fetch('/static/lang.json')` 必须改相对（否则经代理 404 静默变英文）；选项弹窗 89 个选项全汉化（查找加空白归一化兜底多行 help）；配置区改横线 tab
- clash：ttyd 只绑 127.0.0.1（**ttyd 客户端路径同样从 `window.location.pathname` 构造，天然兼容剥前缀，不需要 `-b`**）；页面"高级模式"改开 `/plugin_proxy/clash/`

**28 插件审计结论**：零鉴权已修=clash ttyd/reqable/mitmweb；有自带认证=adguard 3000（bcrypt，模板密码公开用户应改）、frps 面板 7001（默认 admin/admin 用户应改）、nps/lucky（各自登录体系）；其余无 webui。

## 插件 WebUI 品牌/端口串台：mitmproxy v2.0.1（2026-11-19）

**用户报告**（截图）：「这个 mitmproxy 的页面下面的 webui 变成了小黄鸟的 webui 了」——插件外壳是 Mitmproxy抓包（端口 18070/18071/18072），下面的 iframe 却显示 `Proxying / API debug - MITM / proxy RouterIP:18080 / TRAFFIC / HAR / 编写` 那套 Reqable(小黄鸟) 界面。

**根因（源码+二进制双实锤）**：v2.0.0「Python→Go 重写」实际把 `github.com/GitterBois/proxying`（Reqable/runhooks 同源的 Proxying 引擎）整个复制成 mitmproxy 的 Go 引擎（`plugins/mitmproxy/go/`，Makefile 里 `BINARY := bin/proxying`、`CMD := ./cmd/proxying`），**内嵌 Vite WebUI 没做 mitmproxy 品牌隔离**：
- `web/src/components/app-header.tsx`：`proxyHint = "RouterIP:18080"`（写死 Reqable 端口，而 mitmproxy 实际显式代理是 18070；`config.go` 的 `DefaultProxyAddr` 本来就是 18070，纯粹是前端硬编码串台）、`<h1>Proxying</h1>`、`API debug · MITM`、`proxying.har`
- `traffic-list.tsx` / `rest-panel.tsx` 残留英文 `Traffic` / `Captured traffic` / `No traffic yet` / `Load from capture` / `No collections yet`，与 metadata 承诺的「WebUI 中文原生」不符
- `internal/api/export.go` 的 HAR `Content-Disposition` 也是 `proxying.har`
- 已发布二进制 strings 实锤含 `<title>Proxying</title>` 和 `RouterIP:18080`；`web/index.html` 源码虽已改 `<title>Mitmproxy</title>`，但构建时用的 dist 是旧 proxying 的 → 必须重新 `npm run build` + 重编译 Go 引擎才会生效。
- 注意 `X-Proxying-Highlight` / `X-Proxying-Script` 等是引擎内部协议头，**不能改**（改了断功能）。

**修复（v2.0.1）**：品牌/端口/文案隔离（标题「Mitmproxy 抓包」、副标题「流量捕获 · HTTPS 解密」、代理提示 RouterIP:18070、HAR 文件名 mitmproxy.har、lang=zh-CN、英文空态中文化），`npm ci && npm run build` → 复制 dist 到 `internal/webui/dist` → `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w -X main.Version=2.0.1" ./cmd/proxying` 重建内嵌 WebUI 的静态二进制替换 `bins/mitmproxy-go`。

**验证**：新二进制 PT_INTERP 段数=0（静态，uClibc 固件可执行）；字节级检查 `RouterIP:18080`/`<title>Proxying`/`proxying.har` 均为 0 次，`RouterIP:18070`/`Mitmproxy 抓包`/`mitmproxy.har` 各 1+ 次；本地起引擎实测 `GET /plugin_proxy/mitmproxy/` 返回 `<title>Mitmproxy</title>`、内嵌 JS 含全部新文案且无 Proxying/18080，`/api/capture` 返回 `{"paused":false}`；`python build.py` 全量构建通过，新 ipk 内二进制 md5 与编译产物一致。

**发布**：main 分支提交触发 `build-and-deploy.yml`（paths 含 `plugins/**`）→ peaceiris 部署 gh-pages；设备点「更新软件源」后 mitmproxy 显示 2.0.1，升级即生效。

## 「点了更新软件源 → 存储路径丢失」根因：releasenotes 双引号污染 show 响应（2026-11-19）

**现象**（设备实锤，Build202611191110）：点「更新软件源」后插件市场页显示「请先设置插件存储路径 / 未发现可用存储」，看不到 mitmproxy 更新和 runhooks，用户以为存储又丢了。

**只读诊断实锤（设备 API）**：
- `plugin_pgstore show TYPE=data` 返回的 **JSON 非法**：`"installed":[{...01.ctpanel...},,{...reqable...}]` —— `},,` 一个空元素，前端 `JSON.parse` 整体失败 → Vue 状态全空 → 页面渲染成**初始模板**（未设置存储 / 无候选卷 / 旧插件列表），与真实配置无关。
- 容错解析后真实值：`storageSet="yes"`、`storagePath=/etc/disk_user/plugins/plugins_installed`、`storageCandidates` 有 docker/other/plugins 三个卷；`disk_mgmt show` 里 sda7(plugins) 绑定行完好。**数据卷、绑定、标记、已装插件全没丢，纯显示事故**。
- 空元素来自 `plugin_pgstore.sh __show_data`：`_json=$(echo $_json | jq ".newversion = \"$newversion\" | .releasenotes = \"$releasenotes\" | ...")` 把**在线清单的 releasenotes 直接字符串插值进 jq 表达式**（未转义）。mitmproxy 2.0.1 的 releasenotes 含 `{"paused":false}` 里的裸双引号 + 换行 → jq 语法错误输出空 → `_json` 被清空 → `installed+="${installed:+,}$_json"` 拼出空项 → `},,`。
- 只在 `compare_version` 判定「有新版」时才走这条 jq 注入，所以**恰好在发布 2.0.1、用户点更新软件源拉到新清单后爆发**（旧版版本号相同不触发）。用户直觉「像是点击更新软件源导致」精准。

**修复（发布侧 + 固件侧双保险）**：
1. 发布侧：mitmproxy bump **2.0.2**，releasenotes 改纯文本（无裸双引号/反斜杠/换行）；clash/reqable/runhooks 的 releasenotes 里裸双引号一并净化；`build.py` 新增 `validate_metadata()` 门禁，`releasenotes`/`upgradetype` 含 `"` `\` 换行 反引号 `$` 直接拒绝构建（A/B 实测：正常放行、危险拦截）。
2. 固件侧（见 /opt/ikuai4-enterprise AGENTS.md）：`__show_data` 的 jq 注入改 `--arg` 传值 + `_json` 空保护；`updateSoftwareSouce` 的 wget 失败不再写空缓存。

**教训**：插件市场整页状态依赖**一个** JSON 响应，任何单个插件的脏 metadata/releasenotes 都能把整包响应打坏，伪装成「存储丢失」这类完全不同的故障。排查「页面显示成初始模板」先看**原始响应是否合法 JSON**，别去动磁盘/分区。

## 发布侧防复发清单（2026-11-19，每次发版照做）

1. **`metadata.json` 的 `releasenotes` / `upgradetype` 禁止裸双引号、反斜杠、换行、反引号、`$`**：固件 `plugin_pgstore.sh __show_data` 会把在线字段拼进 jq 表达式（新版已改 `jq --arg`，但设备可能跑旧固件、第三方源也不受控），一个双引号就让 `installed` 数组多出空元素 → 整包 `show` 响应非法 JSON → 前端误报「请先设置插件存储路径 / 未发现可用存储」。`build.py` 的 `validate_metadata()` 是硬门禁，CI 会拒绝构建；A/B 测试（正常放行 / 危险拦截）随每轮改动重跑。
2. **发布后验收三件事**：① GitHub API `commits/gh-pages` 的 message 里 head_sha 是不是本次 main SHA（确认 CI 真部署了，别只看 `git push` 成功）；② API `contents/plugins.json?ref=gh-pages` + `Accept: application/vnd.github.raw` 读线上清单，验版本与 releasenotes 无危险字符（**绕开 raw.githubusercontent.com 的缓存**，它曾 5 分钟仍返回旧清单）；③ 下载 ipk 解包，验二进制 md5 与本地编译产物一致、静态无 PT_INTERP（uClibc 固件能执行）。
3. **换底包 / 复制引擎必查品牌残留**：mitmproxy v2.0.0 把 `proxying`（小黄鸟/Reqable 同源）引擎整个复制过来，内嵌 WebUI 标题仍是 `Proxying`、代理提示写死 `RouterIP:18080`（实际 18070）、releasenotes 承诺中文却残留英文。改品牌必须**同时**改 `web/src` → `npm run build` 重建 dist → `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` 重编译内嵌 WebUI 的引擎；只改源码不重编二进制等于没改（已发布包的 strings 实锤含 `<title>Proxying</title>`）。引擎内部协议头（`X-Proxying-*` 等）不能改，改了断功能。
4. **版本号变化才会触发设备 `compare_version` 升级链**：发布新版本前把这条链当测试用例，专门喂带特殊字符的 releasenotes；发布后确认设备 `installed[].newversion` 与在线清单一致。
5. **插件市场页面显示成初始模板 ≠ 存储丢失**：排查顺序、只读诊断命令与固件侧根治见固件仓库 `AGENTS.md §20`。本仓库只负责发布侧不产出脏数据。

## 插件下线记录：mitmproxy（2026-11-19，用户确认）

commit `13c1c26` 删除整个 `plugins/mitmproxy/`（187 文件）。理由：抓包/MITM 能力已整合进 runhooks 一体化插件。**用户已明确「mitmproxy 插件要下线掉」，不要恢复目录，也不要把历史复盘里的案例当成待办去维护。**

**下线机制**：`build.py main()` 遍历 `plugins/` 下的子目录逐个构建并生成 `plugins.json` —— **删掉目录就不再生成条目、ipk 与 logo，无需维护黑名单/开关**。CI（peaceiris 部署 gh-pages）会同步清理线上 ipk。本次已核验：线上清单 28 条无 mitmproxy、ipk 56 个无残留、最后部署 `0bfe34f`（来源 main `6102a91`）。

**每次下线后的双向核验清单**：① 本地目录不存在；② GitHub API `contents/plugins.json?ref=gh-pages` + `Accept: application/vnd.github.raw` 无该 `name`（**绕 raw CDN 缓存**）；③ `contents/ipk?ref=gh-pages` 无该插件 ipk；④ `commits/gh-pages` 最后 message 的 head_sha 是下线后的 main SHA（确认 CI 真部署了）；⑤ 固件侧 grep 无硬编码路由。

**保留但需识别的残留文字（都不是插件引用，不要误删）**：
- `build.py` 注释里的 mitmproxy 案例（uClibc 例子、jq 事故复盘）。
- `docs/mitmproxy-testcases.md`、`docs/插件验收报告-2026-11-18-mitmproxy1.0.2-reqable1.0.42.md` 等历史验证文档，保留作复盘。
- `runhooks/service.sh` / `reqable/service.sh` 里的 `mitmproxy-ca-cert.pem`、`mitmproxy-ca-cert.p12` 是 **CA 证书文件名兼容**（读取旧证书让用户不必重装 CA），属功能代码，**严禁删除**。
- `runhooks/metadata.json` 描述里「整合 mitmproxy 与小黄鸟能力」是产品措辞。

**设备侧**：已安装的 mitmproxy 继续可用，但不再有新装/更新入口；引导用户改装 runhooks 后卸载旧插件。固件侧无硬编码 `/plugin_proxy/mitmproxy/`，无需为下线改固件。

---

## 插件标识大小写：设备升级链的匹配键（2026-11-19，runhooks v1.1.2 → v1.1.4 实锤）

**现象**：本地 HEAD = origin/main = gh-pages 部署 SHA，线上 `plugins.json` 已是新版、ipk 可下载、设备软件源 URL 正确且缓存里也刷到了新清单，但插件市场里已装的 `runhooks 1.1.0` 始终不出升级按钮，新版本反而以 `Runhooks` 另起一条出现在「可安装插件」。表象像「没推送 / CDN 缓存（10 分钟）/ 软件源 URL 残留」，实际都不是。

**根因**：17:58 的改名派单把 `metadata.json` 的 `name` 由 `runhooks` 改成 `Runhooks`（产品品牌改名），并连带改了页面 `func_name: 'plugin_Runhooks'`、`service.sh` 的 `PLUGIN_NAME=Runhooks`、`build.py` 产出的 ipk / plugins.json / logo 命名；当时已留过一句「改名后旧标识的设备不会识别为同一插件的升级，留档待评估」，但没当场处理，2 小时后爆发。固件 `plugin_pgstore.sh __show_data` 用 `jq select(.name == "$name")` **区分大小写**匹配在线清单，`upgrade_online` 也按 name 查缓存 → 已装项 `runhooks` 匹配不到 `Runhooks` → 不注入 `newversion` → `compare_version` 失败 → 不渲染升级按钮；旧安装项成孤儿，手工点升级会拼出空版本 URL 404。**改名当时「刻意保留 `/plugin_proxy/runhooks/` 小写前缀」也是同类隐患**：固件/Go 引擎侧按小写硬编码，跨仓库标识符一旦不同步就是另一处断裂。

**修法（v1.1.4 / `4267578`）**：`name` 改回小写 `runhooks`，同步回 `plugin_runhooks` / `PLUGIN_NAME=runhooks`，version `1.1.3`→`1.1.4`（必须高于线上，否则 compare_version 不注入升级字段），补 `upgradetype:"upgrade"`，releasenotes 重写过 `validate_metadata()` 门禁；目录名不变 → 配置/CA 不丢。同时 `build.py` 改为「仅对声明了 upgradetype 的插件在 plugins.json 输出该字段」，避免 `null` 传给 `install.sh` 导致升级后引擎不重启（stale-proc 坑）。已按大写名**全新安装**过的设备没有旧名安装项，无平滑路径，只能卸载重装。

**注意事项（改任何插件 / 接到改名派单前照做）**

1. **`name` 是设备侧主键，不是可改的产品名**：中文名/品牌改名只动 `alias`、`description`、`releasenotes`、WebUI 标题；`name` 必须与 `plugins/<目录名>/` 及**设备已装版本的字面值**逐字节一致（含大小写）。别做「首字母大写规范化」这类顺手改动。
2. **标识符四处（+框架派生）同改，缺一即断**：`metadata.json name` ↔ `html/index.html` 的 `func_name: 'plugin_<name>'` ↔ `script/service.sh` 的 `PLUGIN_NAME=<name>` ↔ 目录名；框架据此派生 `/usr/ikuai/function/plugin_<name>` 软链、`/plugin_proxy/<name>/` 命名空间、ipk/plugins.json/logo 文件名、包内路径。改完 `grep -rn 'Runhooks\|plugin_Runhooks\|PLUGIN_NAME=Runhooks'` 全仓回扫，残留 0 才提交；固件仓库 `/opt/ikuai4-enterprise` 与 Go 引擎侧的前缀也要一起核，跨仓库不一致同样断链。
3. **凡设备侧按字符串比较的标识一律视为大小写敏感**：name / func_name / PLUGIN_NAME / 目录名 / 软链 / plugin_proxy 前缀 / 清单条目名 / ipk 文件名。Linux 文件系统同样区分大小写，目录名大小写一变 `ln -sf` 就指向不存在的路径，报 `No such file or directory`——与「磁盘不足解压失败」是同族假象（见 §2.2）。
4. **改名派单当场评估升级链，不许「留档待评估」**：先问一句「设备上已装的是哪个字面 name、改完还能不能匹配上」，答不上就别改。当时留的那句话 2 小时后就变成了线上事故。
5. **排查「推送了但设备不出升级按钮」的顺序**：先只读拉 `plugin_pgstore show TYPE=data`，把 `installed[].name` 与线上 `plugins.json` 的 `name` **逐字节比对**，并确认响应是合法 JSON（脏 releasenotes 会整包打坏，见上一节）；不一致就是标识断裂，别去等 CDN 缓存、别去动软件源 URL、别重复 push。
6. **改回 name 必须同时升 version**：`compare_version` 是严格语义化比较，新号不大于线上旧号就不注入升级字段，写成比线上更低比不升更糟（§2.6）。
7. **升级链自检**：本地 `python3 build.py plugins build` 后验 `build/plugins.json` 里该插件只有一条、`name`/`version`/`upgradetype` 正确，解包 ipk 核对包内 metadata/index.html/service.sh 三处标识符一致；发布后按「发布侧防复发清单」四步验收，确认设备 `installed[].newversion` 出现、升级后引擎 stop+start、配置/CA 保留。

## 插件外壳页布局优化纪律（runhooks / 巡流 NetLoom，2026-11-20）

本轮用户「把巡流 NetLoom 的页面也优化一下布局」承接固件插件管理页重设计。锁定实体：`plugins/runhooks/html/index.html` 外壳页（iframe 内呈现，非固件 SPA、非引擎内嵌 WebUI）。

- **只整块替换 `<style>…</style>`**：用 `re.search(r'(<style>\n)(.*?)(\n    </style>)', src, re.S)` 精确换；改前备份后断言 `style 之外完全一致`。本轮实测 `@click 35 / v-model 32 / v-if 31 / v-show 6 / v-for 8 / :class 6 / window.open 0 / target 0 / div 63/63` 全部不变，`node --check` 通过。
- **紧凑节奏对齐固件页**：卡片内距 `18px 20px → 12px 16px`、头部 `padding-bottom 14→10`、`.h20` `14→8px`、`.runhooks-row` `margin-bottom 10→8px`、tab 面板 `16/18→12/14`。硬规范不动：`body{background:#e9edf4}` + 白卡 8px 圆角/描边/阴影、16px 外契约、`#app`/`.box` 滚动语义、tab 纯 HTML+Vue、启停单按钮、引擎未启动 iframe 门控。
- **长说明文字墙用 CSS 收敛，不删文案**：观测步骤 6 步说明 `#rq-settings>.runhooks-tip` 设 `max-height:3.5em; overflow:hidden` + `:hover/:focus-within{max-height:1400px}` + `::after`「悬停展开完整步骤」渐隐标签；`.nl-actions-hint` 与服务区端口行的长提示收敛为单行省略 + hover 展开。既减少首屏占用又不丢信息（skill 硬规范「不改文案」）。订阅面板里的短提示不折叠（用 `#rq-settings>` 直接子选择器精确限定）。
- **状态组 chips 化**：`.nl-status` 加浅底 pill（`border-radius:20px`），4 个 el-tag + 计数 span 统一等高；分区头 `.nl-section-label` 字号 14px/700 + 左侧主色条，与固件 `.sec_head` 同节奏。

**真浏览器 A/B 的坑（本轮实踩）**：
1. mock 桩的固件 www 来自可写镜像挂载点（`/tmp/fix_p`），构建脚本末尾会 `umount` —— 跑渲染前**重新挂载**，否则 `vue.js` 404 → `Vue is not defined` → dump-dom 输出未编译模板，A/B 全是假数据（按钮文本里出现 `{{ }}` 就是信号）。
2. 桩必须模拟 nginx `gzip_static`（`.js`→`.js.gz`）且**显式回 `Content-Encoding: gzip`**；只映射文件不加响应头会让浏览器按明文解析 gzip 字节，同样 Vue undefined。
3. 必须断言 `errs:0`（head 最早处覆写 `console.error`/`onerror` 收集）、`iframe VISIBLE`、按钮文本集合改前改后一致、tab 数一致、计算样式关键值变化；缺一项都说明桩没跑通。
4. **高度断言不能用 `document.body.scrollHeight`**：`#app` 是固定视口高度、`.box` 自己滚的契约，body.scrollHeight 恒等于视口（本轮两版都 913）。改测 `.box_block>.margin_phone` 与 `#rq-settings` 的 `offsetHeight` —— 实测卡片 `1322→1215`、配置区 `436→335`（-23%），紧凑效果才证得出来。

**交付边界（遵守 skill「布局改动是源码层，不是发布」）**：布局改动当轮不升 `metadata.json` 版本、不 commit/push、不打包 ipk、不触发 CI；收尾 `git status --short` 只允许 `plugins/runhooks/html/index.html` 一个文件被改（工作区里 Go 侧 CORS 改动属别的成员，不动、不捎带）。后续用户明确要求发布时再走完整上线链，见下一节。

**纠正一处易错口径**：`html/` 外壳页由 `build.py add_dir` 原样打进包，**不需要重建 WebUI / npm**，但 `build.py` 的 `check_engine_version()` 会比对 `bins/runhooks-x86_64` 内嵌的 `-X main.Version=` 与 `metadata.version` —— **只要 bump 版本就必须重编 Go 引擎**，否则打包直接 `SystemExit` 拒绝。v1.1.9 实踩：先按「只改 html 不用重编」判断，门禁报错才补。


## v1.1.9 实际发布记录（2026-11-20，用户「插件提交推送」）

- **提交** `e438d21`，只 add 4 个文件：`AGENTS.md`、`plugins/runhooks/html/index.html`、`plugins/runhooks/metadata.json`、`plugins/runhooks/bins/runhooks-x86_64`。工作区里别人未提交的 `go/internal/rules/hooks.go` + 3 个 CORS 文件**留在原地未动、未捎带**，`git add` 逐个点名而不是 `-A`。
- **bump**：`1.1.8 → 1.1.9`（先读真值断言旧号，再写；必须高于线上 1.1.8，否则 `compare_version` 不出升级按钮）；`name` 保持逐字节小写 `runhooks`（设备升级链主键，大小写敏感）、`upgradetype: upgrade` 保留；`releasenotes` **只在末尾追加本轮一句**（并发纪律：不整段覆盖别人的说明），且无裸双引号/反斜杠/换行/反引号/`$`。
- **重编引擎（干净源码，避免把别人的半成品编译进去）**：`git archive HEAD plugins/runhooks/go plugins/runhooks/metadata.json | tar -x -C /tmp/rhbuild` 导出 HEAD 干净 Go 源码 → 临时目录写入 bump 后 metadata（Makefile `VERSION` 从 `../metadata.json` 单点读取）→ `export PATH=/usr/local/go/bin:$PATH` 后 `make build-go`（Makefile 直接调 `go`，不带 PATH 会 `go: not found`）→ 产物 `20508856` 字节、内嵌 `1.1.9`、`PT_INTERP=0` 静态、CORS 字符串 0 次 → 拷回 `bins/runhooks-x86_64`。
- **本地打包**：`python3 build.py /tmp/oneplug /tmp/rhverify` 单插件验证通过；`plugin-runhooks-x86_64-v1.1.9-Build202611200828.ipk` 内 html/metadata/二进制三处一致，`validate_metadata()` A/B（正常放行 / 危险拦截）复跑通过。
- **发布验收四步全过**：① CI run `44702926137` `conclusion=success`；② gh-pages 最新部署 `bec88ad6` message 含本次 main SHA `e438d219...`；③ API `contents/plugins.json?ref=gh-pages` + `Accept: application/vnd.github.raw`（绕 raw CDN 缓存）读到 `runhooks 1.1.9 / build 202611200829 / upgradetype upgrade / releasenotes 无危险字符`；④ 下载线上 ipk `7849679` 字节解包，10 项断言全 ✓（html 新 CSS 标记/单行省略/紧凑内距、metadata version/name/upgradetype/releasenotes、二进制内嵌 1.1.9/PT_INTERP=0/无 CORS 串）。
- **API 小坑**：`commits/gh-pages` 返回**单个 commit 对象**（dict），拿它当 list 遍历会 `TypeError: string indices must be integers`；查部署历史要用 `commits?sha=gh-pages&per_page=N`（返回 list）。
- **设备侧下一步**：点「更新软件源」→ 已装 1.1.8 应出现 `newversion 1.1.9` 升级按钮 → 升级走保留配置分支，配置与 CA 证书不丢。

## v1.1.10 左右分栏 tab：消除嵌套边角（2026-11-20）

**用户反馈**：`upload_krdy1mik.png` 问「出现了多久嵌套的多余的边角的情况了」；`upload_y65hpvd9.png` 红框（`x[164,3208] y[509,667]`）圈出「服务与端口」折叠条 +「观测步骤」提示条，要求「不要占据额外的空间。和下面的做成左右的 tab 切换」。澄清问题超时未答，按推荐方案（左侧竖排 tab + 右侧内容面板）落地，已在回复里说明一句话可改口径。

**溯源（`git log -S`，回答「多久」）**：`.runhooks-tip` / `.rq-tabpanel` 自带边框从 `72e3a06`（v1.0.0, 11-18）就有；`.rq-service` 卡片边框从 `de24ee5`（v1.1.3, 11-19）引入；**外层白卡的圆角+描边+阴影（`box-shadow:0 2px 12px`）只在 `e438d21`（今天 v1.1.9）出现** —— 多层「边角套娃」是 v1.1.9 把外层做成显眼白卡后才暴露，内层两块更早。

**改造**（`plugins/runhooks/html/index.html`）：
- 设置区改 `.nl-split` 左右 grid（`152px minmax(0,1fr)`）：左列 `.nl-tabs` 竖排 7 项（服务与端口 / 使用说明 / 观测设备(计数) / 网络范围 / 解密范围 / 记录范围 / 规则订阅），右列 `.nl-panel` 单一内容面板。窄屏（<820px）左列变横向可滚动。
- 框选两块**整块搬进** `activeTab==='service'` / `activeTab==='help'` 面板，DOM / 绑定 / 文案原样搬，运行态默认完全不出现在顶部。
- **只留外层白卡一层边角**：`.nl-panel .rq-tabpanel` / `.rq-service` / `.runhooks-tip` 全部 `border:0!important; border-radius:0!important; background:transparent`，`.nl-split` 只留一条顶部分隔线 + 左列底色；`.runhooks-tip` 只保留左侧 3px 主色条（装饰线，不是完整边框）。使用说明面板里步骤完整显示，不再单行省略。

**不自锁硬约束（关键）**：把「启动引擎」藏进默认不显示的 tab 会让引擎永远起不来。data 加 `tabInitDone:false`，`loadInitData()` 首次数据回来按 `status` 落一次 tab：停止态 → `service`（启动入口一步可点），运行态 → `devices`；之后用户手动切换不再被覆盖。停止态 `details :open="status !== 1"` 仍自动展开。

**功能零回归验证**：原有 `@click` / `v-model` / `v-if` / `v-for` 表达式集合**全部保留**，只新增 2 个 tab 切换 `@click` + 2 个面板 `v-show`（用户明确要求的新 tab 项）；`div` 配平 63→68；`node --check` 通过。
真 Chromium A/B（桩支持 status=1/0 两态、Referer 传状态）：改后 `splitDisplay=grid / cols=152px 1142px / leftRight=OK / tabs=7`；`paneBorder 1px→0px`、`serviceBorder 1px→0px`；`settingsH 317/436→264`（约 -40%）；三个 tab 点击切换全 `OK`；停止态 `activeTab=服务与端口` + `启动引擎 VISIBLE` + iframe 门控 `NOFRAME`；运行态 `activeTab=观测设备`（框选内容完全收起）；`console error 0`。OCR 预览实锤左列 7 个 tab 在 x≈66、右侧面板内容从 x≈211 起。

**验证脚本两个坑**：① 服务面板的可见性判据要用通用控件「保存端口」，别用「启动引擎」——运行态渲染的是「停止引擎」，会误报 WRONG；② Vue `v-show` 是 nextTick 异步，点 tab 后必须 `setTimeout(...,80)` 再读可见面板，同步读拿到的是旧面板。

**发布**：`1.1.9 → 1.1.10`，引擎按 `check_engine_version()` 从干净 HEAD 源码重编（内嵌 1.1.10 / PT_INTERP=0 / 无未提交 CORS 串），本地 `build.py` 单插件包内 8 项断言全过。提交 `5fefe5b`；验收四步全过：CI run `44705595054` success、gh-pages 部署 `1d0a87bf` 含 `5fefe5b…`、API raw 线上清单 `1.1.10 / build 202611200849 / upgradetype upgrade / releasenotes 无危险字符`、线上 ipk `7851349` 字节解包 11 项断言全 ✓（前两次下载 404 是 Pages CDN 传播延迟，第三次成功）。设备点「更新软件源」→ 升级按钮 → 升级（保留配置/CA）。

## v1.1.11 口径纠正：用户说的「左右的 tab」= 横向 tab 行（2026-11-20）

**理解错了一次，记牢**：v1.1.10 把「和下面的做成左右的 tab 切换」理解成**左右分栏**（左侧竖排 tab 列 + 右侧内容面板），用户 `upload_ci499zqh.png` 用红框 `x[90,315] y[326,757]` 圈住整条竖列，回一句「这个还是改成左右的 tab」—— 意思是 **tab 项要横向左右排开**（普通横向 tab 行），不是布局左右分栏。上一轮 clarify 超时未答，按推荐方案（选项 1）做错方向；用户连续两次用截图纠正，说明「左右 tab」在这套语境里一律先按**横向排列的 tab** 理解，别默认侧边栏。

**改动：纯 CSS，DOM 一字未动**（`plugins/runhooks/html/index.html`）：
- `.nl-split` `display:grid(152px 1fr)` → `display:block`（不再分栏）
- `.nl-tabs` `flex-direction:column` → `row` + `flex-wrap:wrap`，底部一条分隔线；`.nl-tab` 选中态从左侧 3px 色条改为底部 3px 色条 + 上方圆角
- `.nl-panel` 内距 `12px 14px` → `12px 0 0`，面板落在 tab 行下方
- **保留** v1.1.10 的两项成果：7 个 tab 项（框选两块仍在 tab 里、运行态不占顶部）、内层 `border:0!important` 只留外层白卡一层边角；`tabInitDone` 停止态自动落服务 tab 保证「启动引擎」可点，逻辑不动
- 窄屏 media query 改为只缩 tab 内距（横向行本就自动换行）

**验证**：`style` 之外与 v1.1.10 完全一致，`@click 37 / v-model 32 / v-if 31 / v-show 8 / v-for 8 / :class 8 / div 68:68` 全部相等，`node --check` 通过。
真 Chromium 两态 A/B：改前 `splitDisplay=grid / tabsDir=column / tabSameRow=DIFF_210(不在同一行)`；改后 `splitDisplay=block / tabsDir=row / tabSameRow=OK(7 项同一行) / panelBelow=OK`；`settingsH 264→201/185`；内层边框仍 `0px`；两态 tab 切换 3/3 OK、停止态 `启动引擎 VISIBLE`、运行态落在观测设备、可见按钮文本集合前后一致、`console error 0`。OCR 预览实锤 7 个 tab 全在 y≈193-214 同一行（x 从 59 到 582 横向排开），面板内容从 y≈240 起。

**A/B 脚本坑**：复制旧脚本改端口时**只改了 server 绑定端口、没改 URL 里的端口** → chromium `ERR_CONNECTION_REFUSED`、`--dump-dom` 返回空 → 四组全「无探针数据」。改端口要同时改 `url=f'http://127.0.0.1:<port>/...'`，调试先打印 `returncode` + `stderr`。

**发布**：`1.1.10 → 1.1.11`，引擎按 `check_engine_version()` 从干净 HEAD 源码重编（内嵌 1.1.11 / PT_INTERP=0 / 无未提交 CORS 串），本地 `build.py` 单插件包内 7 项断言全过。提交 `f94a9f0`；验收四步全过：CI run `44708004935` success、gh-pages `092d1992` 含 `f94a9f0…`、API raw 线上清单 `1.1.11 / build 202611200908 / upgradetype upgrade / releasenotes 无危险字符`、线上 ipk `7851394` 字节解包 11 项全 ✓（复核包内 metadata 与二进制内嵌版本均为 1.1.11）。设备点「更新软件源」→ 升级 → Ctrl+Shift+R 强刷（html 文件名未变）。

## 代理 CORS 跨域修复复核与提交（2026-11-20，用户「看看是不是其他 agent 修复了」）

- **来源**：`git status` 里 4 个遗留文件（`internal/rules/hooks.go` 改 13 行 + 新增 `cors.go` / `cors_test.go` / `internal/proxy/cors_integration_test.go`）从未提交过，是共享工作区里别的成员留下的未提交改动（符合用户猜测），不是已合入的修复。注释标注 `[cors-fix]`，复现的是 1.1.8 真机「使用代理时部分跨域请求报 CORS 错」。
- **根因链（`cors.go` 注释实锤）**：内置 `builtin-cors` 规则（`defaults.go` 里 `Access-Control-Allow-Origin: *`）对所有响应无条件 Set 三个通配头 → ① credentials 模式 ACAO 不能是 `*`；② ACAH/ACAM 的 `*` 对凭证请求同样不被接受；③ 覆盖源站本来就正确的具体 ACAO；④ 无 Origin 的同源响应被注入且无 `Vary: Origin` 污染缓存；⑤ OPTIONS 预检转发源站常被 403/405；⑥ Chrome PNA 预检需 `Access-Control-Allow-Private-Network`。
- **修法**：把 `*` 当「按请求动态展开」的标记值而非字面回写；`isWildcardCORS` 只识别响应阶段 ACAO=`*` 的规则（用户自定义具体值规则走原 `applyHeader`，语义不变；老装机 rules.json 里固化的旧规则升级即生效）；`applyWildcardCORS` 回显 Origin + `Allow-Credentials: true` + ACRM/ACRH、尊重源站具体 ACAO、加 Vary、处理 PNA；`isPreflight` + `preflightResponse` 本地 204 应答，放在 `OnRequest` 所有规则之后（不绕过 host_block/map_local）。
- **复核（不是只看能不能编译）**：`defaults.go` 确有 `*` 标记；`bytesResp` 返回非 nil Header；`go build ./...` 通过；**全量 `go test ./...` 全绿**；`internal/proxy/cors_integration_test.go` 是真端到端（起 httptest 源站 OPTIONS 回 405 + `NewChain` 挂引擎 Rules 钩子 + 真 `http.Client` 走代理），断言带凭证 GET 回显具体 Origin/ACAC、预检本地 204 且**源站收到预检 0 次**、ACAM=PUT / ACAH=x-token。规则单测另覆盖回显/保留源站/无 Origin 不注入/停用规则不短路/无 Origin 不短路。
- **已提交推送** `9227d1f`（4 个源码/测试文件，无二进制；`go/bin/` 被 .gitignore 忽略）。发布链复核：CI run `44708892520` success、gh-pages `3d71f0ce` 含 `9227d1f…`、线上清单仍 `1.1.11 / build 202611200913`、线上 ipk `7851392` 字节解包 5 项 ✓。
- **坑（务必记住）**：`build.py` 只把 `bins/` 下**已编译**的二进制打进 ipk，提交 `go/` 源码**不会**让线上包变化 —— 字节级实锤包内二进制仍是 1.1.11 且不含 `wildcardCORSEnabled`/`applyWildcardCORS`。所以**设备上的 CORS 报错目前仍未解决**：要生效必须用含本次 CORS 改动的 HEAD 源码重编引擎（`make build-go`）、按 `check_engine_version()` bump 到 1.1.12、再打包发布，设备升级后才吃到修复。别把「源码已提交」当成「问题已修好」。

## v1.1.12 代理 CORS 修复正式生效（2026-11-20，用户「继续解决」）

承接上一节：源码 `9227d1f` 已合入，但线上 ipk 内二进制仍是 1.1.11、不含修复。本轮把修复编进引擎并发布，设备升级后生效。

**链路**：
1. 主仓库 metadata `1.1.11→1.1.12`（写前重读断言旧号；releasenotes 只追加一句、过 `validate_metadata()` A/B；`name`/`upgradetype` 不动）。
2. `git archive HEAD plugins/runhooks/go | tar -x -C /tmp/rhbuild4` 导出**含 CORS 改动的 HEAD 源码**（`9227d1f` 已提交），把主仓库新 metadata 拷到 `/tmp/rhbuild4/plugins/runhooks/metadata.json`；`export PATH=/usr/local/go/bin:$PATH` 后 `make build-go`（Makefile 的 `VERSION` 从 `../metadata.json` 单点读取）。**这次绝不能再用「不含 CORS 的干净源码」** —— 那正是上一版二进制没修复的原因。
3. 二进制：内嵌 1.1.12、`PT_INTERP=0`、体积 `20508856→20517048`（+8192 字节，代码确实编入）。
4. 拷回 `bins/runhooks-x86_64`，本地 `build.py` 单插件包内 8 项断言全过。
5. 提交 `0ce8699`（只 2 个文件：metadata + 二进制；html 本轮未改），推送。

**验证判据纠正（重要）**：前几轮用 `b.count(b'wildcardCORSEnabled')==0` 判「二进制不含 CORS 修复」**不可靠** —— `go build -ldflags "-s -w"` 剥离符号表，函数名字符串无论是否编入都可能为 0，那个 0 不能当证据。改用**修复独有的字符串常量**：`Access-Control-Allow-Private-Network`、`Access-Control-Allow-Credentials`、`Access-Control-Max-Age`、`GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS`；`git grep HEAD~2` 确认修复前源码无这些串 → 二进制里出现即铁证。本轮本地与线上 ipk 均验到。

**真实引擎冒烟（最强证据）**：用刚编译的二进制起引擎（`-proxy-addr 127.0.0.1:19170 -api-addr 127.0.0.1:19171 -data-dir /tmp/rhsmoke_data`）+ 本地源站（OPTIONS 故意回 405），`curl -x --noproxy ""` 走代理发请求：OPTIONS 预检本地 **204**（不是源站 405）、ACAO 回显 `https://app.example.com`、ACAC=true、ACAM=PUT、ACAH=x-token、Max-Age=600、三个 Vary；带凭证 GET 200 且源站 body 正常、ACAO 回显具体 Origin。**7 项全过**。这才是「问题解决」的判据，不是「源码已提交」。

**发布验收**：CI `44710072551` success、gh-pages `aa0f77af` 含 `0ce8699…`、线上清单 `1.1.12 / build 202611200922 / upgradetype upgrade / releasenotes 无危险字符`、线上 ipk `7855630` 字节解包 7 项全 ✓（含 CORS 修复已编进包内二进制、横向 tab CSS 未回归）。

**API 额度坑**：匿名 GitHub API 60/h 在连续多轮验收后耗尽（`403 rate limit exceeded`）。绕法：① 清单用 `raw.githubusercontent.com/<owner>/<repo>/gh-pages/plugins.json?_=<ns>`（不消耗 API 额度，本轮先读到 1.1.12）；② 部署 SHA 用 `github.com/<owner>/<repo>/commits/gh-pages.atom`（Atom feed，实证 `Deploy plugins build 0ce8699…`）；③ Pages `github.io/plugins.json?nocache=` 有 10 分钟 CDN 缓存、加 query 也滞后，别拿它判「没发布」；④ 查 `api.github.com/rate_limit` 的 reset，等重置后重跑完整四步脚本。

**设备侧**：点「更新软件源」→ 已装 1.1.11 出 1.1.12 升级按钮 → 升级（保留配置/CA）→ 引擎重启后 CORS 报错消除。

## 插件详情浮层文案长度与「不 bump 版本」的有意例外（2026-11-20）

- **`metadata.releasenotes` 不要逐轮无限追加**：每轮 bump 追加一句、五轮后到 1114 字符，固件插件管理页 popover 里 `<p v-html="item.releasenotes">` 全量展示，把底部更新/删除按钮顶出视口（用户截图 `upload_7fpiasfl.png`）。历史说明留在 git / AGENTS.md，**releasenotes 只保留当前版本**，description 控制在 ~100 字内。本轮 `4c8eb3d`：description `198→96`、releasenotes `1114→145`。
- **有意不 bump version 的例外**：本轮改的只是**在线清单展示字段**（description / releasenotes），不是功能或包内文件。设备 `plugin_pgstore show` 的 `onlinePlugins` 在「更新软件源」时实时拉取，不需要 `compare_version` 出新版本 → 保持 `version=1.1.12` 即可生效；`check_engine_version()` 仍通过（二进制内嵌 1.1.12 == metadata），CI 重新打包生成新 build 号 ipk（包内容不变）。**这不违反「每次改插件必须升 version」纪律** —— 那条针对功能/包内文件改动需要设备升级；若改了 `html/` / `script/` / 二进制，仍必须 bump 并按 version-gate 重编。
- 验收：CI `44711936783` success、gh-pages `6b5e47c7` 含 `4c8eb3d…`、API raw 线上清单 description 96 / releasenotes 145 / version 1.1.12 / upgradetype upgrade / releasenotes 无危险字符。设备点「更新软件源」后详情浮层立刻变短，按钮可见。
- 固件侧同时加了通用兜底（浮层 `max-height` + 内部滚动 + 按钮行 sticky），见固件仓库 `AGENTS.md §24`；以后别的插件文案变长也不会挡按钮。

## v1.1.13 全屏观测台铺满整个平台 + 按钮常显（2026-11-20）

**用户反馈**：「现在这个插件没有全屏显示的按钮了」，补充「要求的是铺满整个平台的那种」。

**两个根因**：
1. 按钮 `<button v-if="status === 1" @click="setFullscreen(true)">全屏观测台</button>` —— 引擎停止时整个消失（从 v1.0.0 起就是这条件，不是本轮回归）。A/B 实测 status=0 时 `btn=NO` 精确复现。
2. 旧「全屏」只是插件页内 `.runhooks-frame.is-full{position:fixed;inset:0;width:100vw;height:100vh}` —— `position:fixed` 在嵌套 iframe 里只相对**该 iframe 视口**，跨不出 iframe；A/B 实测父 iframe 仍是 `0,92,1385,753`（平台顶栏占 92px 没盖住），`covers=NO`。skill 旧口径「铺满插件页已满足不弹新窗口的全屏」对这轮用户不适用，用户要的是整个平台。

**修法**（`plugins/runhooks/html/index.html`）：
- 模块级 `setFrameFullscreen(on)`：同源拿 `window.frameElement`（固件 SPA 嵌入插件页的那个 iframe），全屏时 `setProperty` 加 `position:fixed / inset:0 / width:100vw / height:100vh / z-index:99999 / border:0 / background:#fff`（全部 `important` 压过 SPA 内联），把**承载 iframe 本身**提升到整个视口；退出时用保存的 `__frameStyle` 原样 `setAttribute('style', ...)` 恢复。不用 Fullscreen API（跨 iframe 权限不稳），同源 DOM 操作即可。顶层直接打开时 `frameElement` 为 null，退回页内 `.is-full`，不报错。
- `setFullscreen(on)` 联动调用；`openRewrite()` 也走它，断点改包工作台自动铺满平台；ESC 与「退出全屏(返回本页)」都走恢复分支。
- 全屏按钮去掉 `v-if`，改 `:disabled="status !== 1"` + `:title` 提示「请先启动引擎」—— 常显但引擎没跑时点不动，iframe 门控仍隐藏（不会出现 Chromium 连接错误页）。
- parity：`@click 37 / v-model 32 / v-for 8 / :class 8 / div 68:68 / window.open 0 / target 0` 全相等，仅少 1 个 `v-if`、多 1 个 `:disabled`；`node --check` 通过。

**真浏览器 A/B**（写父页面模拟平台：60px 顶栏 + `<iframe id=pgframe>` 嵌插件页）：before status=1 `fsPosition=static / covers=NO_0,92,1385,753,1400,813`；before status=0 `btn=NO`（按钮消失）；after status=1 `fsPosition=fixed / fsZ=99999 / covers=OK / restored=OK`；after status=0 `btn=YES + btnDisabled=YES`；四组 `console error 0`。
坑：`--dump-dom` 对父页面只输出父文档，子 iframe 里写 `document.title` 读不到，必须写 `window.parent.document.title`；`/tmp/fix_p` 固件 www 在构建后会被 umount，跑桩前要重新挂载，否则 `vue.js` 404 → Vue 未定义 → 探针读到未编译模板的假数据。

**发布**：`1.1.12 → 1.1.13`，`releasenotes` **替换**（145→161 字，不追加，遵守「只留当前版本」纪律），引擎按 `check_engine_version()` 从含 CORS 的 HEAD 源码重编（内嵌 1.1.13 / PT_INTERP=0）。提交 `c624e31`；验收四步全过：CI `44715191885` success、gh-pages `5436a512` 含 SHA、线上清单 `1.1.13 / build 202611200958 / description 96 / releasenotes 161 / 无危险字符`、线上 ipk `7854563` 字节解包 9 项全 ✓（含 CORS 与横向 tab 未回归）。设备点「更新软件源」→ 升级 1.1.13 → 全屏按钮常显、点击铺满整个平台。

## v1.1.14 全屏消除 4 条边缘空隙：提升整条 iframe 链（2026-11-20）

**用户反馈**：`upload_wbq2d8z4.png` —— 全屏后仍露出平台顶栏与左侧菜单，「多了框选出来的 4 个边缘空隙部分」。

**根因**：插件页被嵌**两层** iframe（SPA → `02.pgstore` 插件管理页 → 插件功能页 `html/index.html`）。v1.1.13 只提升 `window.frameElement`（最内层 plugframe），只铺满 02.pgstore 那一层；SPA 顶栏(60px) 与左侧菜单(220px) 仍露在外面 = 4 条空隙。两层 iframe 的 A/B 实测 `frames=2`、顶层 iframe `position=absolute`、`covers=NO_220,60,1180,753,1400,813`、`topbarCovered=NO`，与真机截图吻合。

**修法**：
- `frameChain()` 沿 `window.parent` 链收集**每一层** `frameElement`（try/catch 兜跨域），`setFrameFullscreen(on)` 对全部层统一提升：`position:fixed` + `top/left/right/bottom:0` + `width/height:100%` + `margin:0` + `border:0` + `z-index:99999` + `background:#fff`（全 `important` 压过 SPA 内联）；退出时按保存的 `__frameStyles[]` 逐层 `setAttribute('style', ...)` 恢复。
- **坑（实测）**：iframe 是 replaced element，`width/height:auto` 会退回默认 **300×150**（第一版 A/B `covers=NO_0,0,300,150,...`），必须用 `100%`（fixed 元素的 100% 相对父文档视口）；也别只写 `inset:0` 不给尺寸。样式作用在**父文档里的 iframe 元素**上，所以 `100%`/vw 都相对父视口，不是 iframe 自身视口 —— v1.1.13 注释里那句「vw/vh 在 iframe 里相对自身视口」是错的。
- 全屏态给 `body` 加 `platform-fullscreen` class，CSS 把 body padding / `.box` padding / `.margin_phone` padding、边框、圆角、阴影全部归零，观测台 iframe 真正贴边；退出移除 class。
- parity 全等（`div 68:68`、各绑定不变），`node --check` 通过。

**真浏览器 A/B**（两层 iframe 模拟平台：顶层 topbar+sidebar → pgstore iframe → 插件页 iframe，探针写 `window.top.document.title` 才能被 `--dump-dom` 读到）：before `topPos=absolute / covers=NO_220,60,... / topbarCovered=NO`；after `topPos=fixed / topZ=99999 / covers=OK / topbarCovered=OK_pgstore / restored=OK`；四组 `console error 0`。

**发布**：`1.1.13→1.1.14`（releasenotes 替换 161→207 字，仍精简；引擎按 version-gate 从含 CORS 的 HEAD 源码重编，内嵌 1.1.14）。提交 `234fdb7`；验收四步全过：CI `44717763005` success、gh-pages `dc48780f` 含 SHA、线上清单 `1.1.14 / build 202611201016 / description 96 / releasenotes 207 / 无危险字符`、线上 ipk `7854906` 字节解包 11 项全 ✓（CORS 与横向 tab 未回归）。
- 验收脚本坑：从上一版复制脚本时**比较值替换了但标签文字和阈值没同步**（`metadata 1.1.13` 标签、`releasenotes<200` 阈值 vs 实际 207 字）→ 首轮误报 INCOMPLETE；按实际值复核后 PUBLISH VERIFIED。复制验收脚本要连标签和阈值一起改。

**设备侧**：点「更新软件源」→ 升级 1.1.14 → 全屏观测台铺满整个平台，顶栏/菜单不再露出。

## v1.1.15 去掉插件详情页四周灰色间隙（2026-11-20）

- 用户澄清「插件的详情页和上面返回插件列表中间的间隙不需要，上下左右都有灰色间隙」—— 指**普通状态**，不是全屏。两层都要改：插件页 `body` 灰底+`padding:14px 16px` 与白卡边角；固件 `02.pgstore` 的 `.plugin_frame` padding/描边/圆角/上下 20px 灰边 + 标题栏与 iframe 之间的 `<div class="h20">`。
- 插件页 CSS-only：`body{background:#fff!important;padding:0!important;margin:0!important}`、`#app .box`/`.box_block` padding/margin 归零、`.margin_phone` 去 `border/radius/box-shadow` 但**保留内容内距 12px 16px 14px**（白底不是灰边）。**口径反转**：旧「灰底 + 白卡浮起」曾因用户要求背景区分而加，现明确取消，灰色间隙一律不要；文档/skill 同步。
- 两层 A/B（父 pgstore 模拟 + 插件页 iframe）：before `gapPx=20 / plugBodyPad=14px / plugBodyBg=rgb(233,237,244)`；after `gapPx=0 / plugBodyPad=0 / plugBodyBg=rgb(255,255,255)`、iframe `rect=0,41,1400,772` 左右贴边紧贴标题栏；`console error 0`。parity 全等、`node --check` 通过。
- 发布 `1.1.14→1.1.15`（releasenotes 替换 207→171 字，仍精简；引擎按 version-gate 重编内嵌 1.1.15）。固件侧见固件仓库 `AGENTS.md §25`。设备需**同时**刷固件 `Build202611202028` 与升级插件 1.1.15 才完全无间隙。

**内置规则默认全开导致站点 400（v1.1.16，mail.163.com 实锤）**：`defaults.go` 里 `builtin-no-cache` / `builtin-accept-encoding` 是 `Match{}`（命中所有请求）且 `Enabled:true`，会把 `Cache-Control/Pragma: no-cache`、`Accept-Encoding: identity` 发给上游、删掉 `If-None-Match/If-Modified-Since`；高亮规则还把内部标记 `X-Proxying-Highlight*` 写进请求头外泄。设备走代理打开 mail.163.com 全部 400，换站点同样中招。四条防线：① 侵入性全局请求改写默认关闭（引擎 response 阶段已透明解码→替换→重编码，`identity` 规则已冗余）；② **只改 defaults.go 对老装机无效** —— `NewChain/seedDefaults` 明确「rules.json 已存在就不覆盖」，必须在 `Reload()` 加载层按 ID 强制禁用（不写回文件，不动用户其它规则与订阅规则）；③ 引擎 `onRequest` 转发前剥逐跳头（`Proxy-Connection` 等）与内部标记头，但**协议升级请求必须保留 `Connection: Upgrade` / `Upgrade`**，剥掉上游直接回 400（`Test_Engine_WS_captures_frames` 实锤）；④ `onResponse` 把 body 全量读入内存后必须清 `resp.TransferEncoding` + `Transfer-Encoding` 头，再用确定的 `Content-Length` 回写（CL 与 TE 并存是非法响应，严格客户端报 400）。回归测试：`internal/rules/compat400_test.go`（老 rules.json 里 Enabled=true 的规则加载后失效、请求头不被改写）+ `internal/proxy/compat400_test.go`（端到端剥头 / chunked 上游回写只有 CL）。发版仍走 bump→`make build-go`→拷 bins→`build.py` 版本门禁。
