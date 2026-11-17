#!/bin/bash
# [musl-loader] iKuai 固件用户态是 uClibc, 没有 glibc/musl loader, 也没有 Python。
# 本插件自带完整 Alpine(musl) 运行时: Python3.14 + mitmproxy 及其全部依赖。
# 启动方式: 直接用自带的 musl 动态链接器(static-pie, 不依赖宿主机任何 libc)
# 加载 python3.14, --library-path 指向 bundle 内的 lib —— 与宿主 libc 完全无关,
# uClibc 固件上同样跑得起来(原型已在 glibc 宿主验证: mitmweb HTTP 200 + 代理抓流)。
B="$(cd "$(dirname "$0")" && pwd)"
exec "$B/lib/ld-musl-x86_64.so.1" --library-path "$B/lib:$B/usr/lib" \
    "$B/usr/bin/python3.14" "$B/usr/bin/mitmweb" "$@"
