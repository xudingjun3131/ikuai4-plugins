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

## 4. 当前状态（2026-11-17）

- **27 个插件**：adguard clash ddnsgo frpc frps gecoos linkease lucky mds npc nps openp2p pppoe-scan reqable smartdns socat socks5 socks5-extapi syslog tailscale uu vidblock vlmcsd vnt vnts vremote zerotier（alist 已应用户要求下线）
- **全部已完成 storage-anchor 迁移**：54 处 `source /etc/mnt/plugins/configs/config.sh` 改为框架 helper（旧固件回退），version 全 +1（adguard 2.1.7 … zerotier 1.2.2）；需搭配固件 `Build202611171200` 及以上（boot() 会自愈旧版插件的硬编码 source 行，不升级插件也能用）
- **15 个插件没有二进制**（tailscale/zerotier/openp2p/socat/linkease/vlmcsd/uu/vidblock/vremote/mds/pppoe-scan/syslog/socks5-extapi/gecoos/reqable 中部分为未完成的移植桩）——`service.sh` 引用的 `$plugin_dir/../bin/xxx` 在 ipk 里不存在，是移植桩；reqable 是自研引擎（源码 `/opt/reqable`，Go 全静态 + 汉化）
- 固件侧插件框架的完整改动记录在 `/opt/ikuai4-enterprise/AGENTS.md`，坑位在 skill `ikuai-firmware-build`
