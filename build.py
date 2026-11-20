import json
import os, io
import re
import shutil
import struct
import sys
import tarfile
import time

def get_timestamp():
    # [build-utc] v1.1.8: CI(ubuntu-latest, TZ=UTC) 与本地构建机(CST)原先各用
    # localtime, 同一份源码在两端产出的 build 号差 8 小时, 修复报告与线上清单
    # 对不上(测试报告 §四 / 修复报告 §四 记为遗留项)。统一用 UTC 时间戳:
    # 两端一致, 设备按线上清单 build 拼 URL 拉包不再受时区口径干扰。
    return time.strftime('%Y%m%d%H%M', time.gmtime())

def check_elf_interp(src_path: str):
    # [static-gate] 目标固件(iKuai uClibc)没有 glibc loader: PT_INTERP 指向
    # ld-linux-* 的二进制 exec 时内核返回 ENOENT, bash 把它报成 "No such file or
    # directory"(指的是 loader 不是二进制本身, 极常误判成文件丢失/磁盘满/软链错)。
    # 2026-11-18 reqable v1.0.42 实锤: 修 fd 耗尽重建引擎时漏 CGO_ENABLED=0,
    # 发货 glibc 动态包, 设备全部 "No such file or directory" 启动失败。
    # uClibc(ld64-uClibc, 如 mds/pppoe-scan)与 musl(ld-musl, mitmproxy 自带
    # Alpine runtime)放行; 只查 64 位 ELF(32 位解析不了, 跳过不误伤)。
    with open(src_path, 'rb') as f:
        hdr = f.read(64)
        if len(hdr) < 64 or hdr[:4] != b'\x7fELF' or hdr[4] != 2:  # ELFCLASS64
            return
        phoff = struct.unpack_from('<Q', hdr, 32)[0]
        phentsize = struct.unpack_from('<H', hdr, 54)[0]
        phnum = struct.unpack_from('<H', hdr, 56)[0]
        f.seek(phoff)
        for _ in range(phnum):
            e = f.read(phentsize)
            if len(e) < phentsize:
                break
            if struct.unpack_from('<I', e, 0)[0] == 3:  # PT_INTERP
                po = struct.unpack_from('<Q', e, 8)[0]
                pz = struct.unpack_from('<Q', e, 32)[0]
                f.seek(po)
                interp = f.read(pz).rstrip(b'\x00').decode(errors='replace')
                if 'ld-linux' in interp:
                    raise SystemExit(
                        f'\n[static-gate] 拒绝打包 glibc 动态二进制: {src_path}\n'
                        f'  PT_INTERP = {interp}\n'
                        f'  固件只有 uClibc loader, 该二进制在设备上 exec 必报\n'
                        f'  "No such file or directory"(loader 缺失, 不是文件不在)。\n'
                        f'  修法: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build\n'
                        f'        -ldflags "-s -w" 静态重建(纯 Go 也照样链出 glibc\n'
                        f'        interp, 别以"没用 cgo"自我排除), 复查 PT_INTERP 为空。\n')
                return

def get_default_permission(data: str | bytes) -> int:
    # .sh .lua and non extension files need exec permission
    if isinstance(data, str):
        _, ext = os.path.splitext(data)
        if ext in ['.sh', '.lua']:
            return 0o755
    
        f = open(data, 'rb')
    else:
        f = io.BytesIO(data)

    with f:
        header = f.read(2)
        if header == b'#!':
            return 0o755

        header += f.read(2)
        if header == b'\x7FELF':
            return 0o755

    return 0o644

def add_raw(tar: tarfile.TarFile, path: str, data: str | bytes, mode: int | None = None):
    if isinstance(data, str):
        data = data.encode('utf-8')
    fileobj = io.BytesIO(data)
    info = tarfile.TarInfo(name=path)
    info.size = len(data)
    if mode is None:
        mode = get_default_permission(data)
    info.mode = mode
    info.mtime = time.time()
    tar.addfile(tarinfo=info, fileobj=fileobj)

def add_dir(tar: tarfile.TarFile, dir_path: str, dest_path: str, mode: int | None = None):
    # [symlink-fix] 目录符号链接要当链接打包: os.walk 默认不跟随符号链接, 链接到目录的
    # 会出现在 dirs 里但不会被 descend, 也不会进 files —— 静默整个丢掉。这里把链接
    # 目录挪进 files 交给 add_file(add_file 对 islnk/issym 只写 linkname 不 open 目标)。
    for root, dirs, files in os.walk(dir_path):
        for d in [d for d in dirs if os.path.islink(os.path.join(root, d))]:
            dirs.remove(d)
            files.append(d)
        for file in files:
            full_path = os.path.join(root, file)
            arcname = os.path.relpath(full_path, start=dir_path)
            arcname = os.path.join(dest_path, arcname)
            add_file(tar, full_path, arcname, mode)

def add_file(tar: tarfile.TarFile, src_path: str, dest_path: str, mode: int | None = None):
    # [symlink-fix] 符号链接必须在最前面处理并直接返回: 后面两处都会跟随链接 ——
    # get_default_permission() 要 open() 读 shebang、addfile 要 open() 读内容, 目标
    # 若是绝对路径(Alpine busybox applet 链接全是 /bin/busybox)就在**打包机**上解析:
    # 打包机恰好有该文件时构建成功, 换台没有的(GitHub CI ubuntu-latest)直接
    # FileNotFoundError 整个构建崩。链接只写 linkname 不带内容, 与目标在不在无关。
    info = tar.gettarinfo(src_path, arcname=dest_path)
    if info.islnk() or info.issym():
        info.mode = mode if mode is not None else 0o777
        tar.addfile(info)
        return

    check_elf_interp(src_path)

    if mode is None:
        mode = get_default_permission(src_path)
    info.mode = mode

    with open(src_path, "rb") as f:
        tar.addfile(info, f)

def build_sigle_plugin(plugin_path: str, output_dir: str, metadata: dict[str, str], arch: str, ts: str):
    if arch == "all":
        full_name = "plugin-{name}-v{version}-Build{timestamp}".format(
            name=metadata["name"],
            version=metadata["version"],
            timestamp=ts
        )
    else:
        full_name = "plugin-{name}-{archs}-v{version}-Build{timestamp}".format(
            name=metadata["name"],
            archs=arch,
            version=metadata["version"],
            timestamp=ts
        )
    
    plugin_meta = {
        "name": metadata["name"],
        "alias": metadata["alias"],
        "manufacturer": metadata["manufacturer"],
        "version": metadata["version"],
        "type": metadata["type"],
        "description": metadata["description"],
        "releasenotes": metadata.get("releasenotes", ""),
        "upgradetype": metadata.get("upgradetype", ""),
    }

    tar_path = os.path.join(output_dir, f"{full_name}.ipk")

    base_dir = '.'

    with tarfile.open(tar_path, "w:gz") as tar:
        add_dir(tar, os.path.join(plugin_path, "html"), f"{base_dir}/html")
        add_file(tar, os.path.join(plugin_path, "logo.png"), f"{base_dir}/html/logo.png")
        add_raw(tar, f"{base_dir}/html/metadata.json", json.dumps(plugin_meta))

        add_dir(tar, os.path.join(plugin_path, "script"), f'{base_dir}/script')
        add_file(tar, os.path.join(plugin_path, "install.sh"), f"{base_dir}/install.sh", mode=0o755)

        bin_dir = os.path.join(plugin_path, "bin")
        if os.path.exists(bin_dir) and os.path.isdir(bin_dir):
            add_dir(tar, bin_dir, f"{base_dir}/bin")

        include = metadata.get("include", [])
        for item in include:
            src_path = os.path.join(plugin_path, item)
            if os.path.exists(src_path):
                if os.path.isdir(src_path):
                    add_dir(tar, src_path, f"{base_dir}/{item}")
                else:
                    add_file(tar, src_path, f"{base_dir}/{item}")
            else:
                print(f"Warning: Included path '{item}' does not exist in plugin directory.", file=sys.stderr)
        
        bins: dict[str, str | dict[str, str] | list[str]] = metadata.get("bin", {})
        if arch in bins:
            bin_files = bins[arch]
            if isinstance(bin_files, dict):
                for src, dest in bin_files.items():
                    src_path = os.path.join(plugin_path, src)
                    dest_path = f"{base_dir}/{dest}"
                    add_file(tar, src_path, dest_path)
            elif isinstance(bin_files, list):
                for file in bin_files:
                    src_path = os.path.join(plugin_path, file)
                    dest_path = os.path.basename(file)
                    add_file(tar, src_path, f"{base_dir}/{dest_path}")
            else:
                src_path = os.path.join(plugin_path, bin_files)
                dest_path = os.path.basename(bin_files)
                add_file(tar, src_path, f"{base_dir}/{dest_path}")

def validate_metadata(metadata: dict, path: str) -> None:
    """发布门禁: 固件 plugin_pgstore.sh __show_data 把 releasenotes/upgradetype
    直接字符串插值进 jq 表达式(见 ".newversion = \"$newversion\" | ..."),
    值里出现双引号/反斜杠/换行会让 jq 表达式语法错误 → jq 输出空 → 安装条目
    被清空 → installed 拼出空元素(},,) → 整个 show 响应 JSON 非法 → 前端
    JSON.parse 失败后显示成初始模板, 误报「请先设置插件存储路径」, 并连带
    看不到任何插件更新/新插件(设备实锤 2026-11-19, mitmproxy 2.0.1 触发)。
    固件侧已改用 jq --arg 根治, 这里再卡一道发布侧, 双保险。
    """
    for key in ("releasenotes", "upgradetype"):
        value = metadata.get(key, "")
        if not isinstance(value, str):
            continue
        bad = [c for c in ('"', "\\", "\n", "\r", "`", "$") if c in value]
        if bad:
            raise ValueError(
                f"{path}: metadata.{key} 含禁止字符 {bad!r} —— 会让固件插件市场 "
                f"jq 注入失败并污染整个 show 响应; 去掉裸双引号/反斜杠/换行后再发布"
            )


def check_engine_version(plugin_path: str, metadata: dict) -> None:
    """[version-gate] Go 引擎内嵌版本必须与 metadata.version 逐字节一致。

    真机实测(D-01): v1.1.3/v1.1.4 只换了前端与元数据、没重编 Go 引擎,
    /api/health 一直报 1.1.2, 而 metadata 和线上 plugins.json 已是 1.1.4。
    根因是 Makefile 的 VERSION 取自 git describe、与插件版本没有强绑定,
    发布链路也没有一道关卡去比对二进制。这里在打包时直接读二进制里
    Go 链接器写下的 -X main.Version=<ver> 并与 metadata 比对: 不一致就
    拒绝构建, 逼出「改版本 → 重编引擎 → 打包」的完整动作。
    只管带 -X main.Version= 的 Go 引擎二进制, 其它二进制(uClibc/musl)
    不受影响。
    """
    bins = metadata.get("bin", {})
    candidates = []
    for spec in bins.values():
        if isinstance(spec, dict):
            candidates.extend(spec.keys())
        elif isinstance(spec, list):
            candidates.extend(spec)
        elif isinstance(spec, str):
            candidates.append(spec)
    want = metadata["version"].encode()
    for rel in candidates:
        path = os.path.join(plugin_path, rel)
        if not os.path.isfile(path):
            continue
        with open(path, "rb") as f:
            blob = f.read()
        if b"-X main.Version=" not in blob:
            continue
        found = set(re.findall(rb"-X main\.Version=([0-9][0-9A-Za-z._+-]*)", blob))
        if found != {want}:
            raise SystemExit(
                f"\n[version-gate] 拒绝打包: {path} 内嵌引擎版本与 metadata 不一致\n"
                f"  二进制内嵌版本: {sorted(v.decode(errors='replace') for v in found)}\n"
                f"  metadata.version: {metadata['version']}\n"
                f"  修法: 改 plugins/<name>/metadata.json 的 version 后, 到 go/ 目录执行\n"
                f"        make build-go  (VERSION 已从 ../../metadata.json 单点读取),\n"
                f"        把 bin/runhooks 拷回 bins/<arch 二进制名>, 再重新打包。\n"
                f"  /api/health.version / 包内 metadata.version / plugins.json.version\n"
                f"  三者必须相等, 否则设备升级后仍报旧版本。\n"
            )


def build_plugin(plugin_path: str, output_dir: str) -> dict[str, str]:
    with open(os.path.join(plugin_path, 'metadata.json'), 'r', encoding='utf-8') as f:
        metadata = json.load(f)
    validate_metadata(metadata, os.path.join(plugin_path, 'metadata.json'))
    check_engine_version(plugin_path, metadata)
    archs = metadata.get("arch", [])
    if not archs:
        archs = ["all"]
    ts = get_timestamp()
    for arch in archs:
        build_sigle_plugin(plugin_path, output_dir, metadata, arch, ts)
    entry = {
        "name": metadata["name"],
        "alias": metadata["alias"],
        "manufacturer": metadata["manufacturer"],
        "version": metadata["version"],
        "type": metadata["type"],
        "description": metadata["description"],
        "releasenotes": metadata.get("releasenotes", ""),
        "build": ts,
        "compatibility": archs
    }
    # [upgrade-chain] 设备 upgrade_online 用 jq -r '.upgradetype' 取值传给 install.sh;
    # 清单缺该字段会得到 null, 升级分支不命中, 引擎不重启(stale-proc)。仅为声明了
    # upgradetype 的插件输出该字段, 其余插件的清单保持原样。
    if metadata.get("upgradetype"):
        entry["upgradetype"] = metadata["upgradetype"]
    return entry

def main():
    if len(sys.argv) != 3:
        print("Usage: python build.py <plugin_source_dir> <output_dir>")
        sys.exit(1)
    
    plugin_source_dir = sys.argv[1]
    output_dir = sys.argv[2]

    if not os.path.exists(plugin_source_dir):
        print(f"Error: Plugin source directory '{plugin_source_dir}' does not exist.")
        sys.exit(1)
    
    if os.path.exists(output_dir):
        shutil.rmtree(output_dir)

    os.makedirs(output_dir, exist_ok=True)

    plugin_dir = os.path.join(output_dir, "ipk")
    os.makedirs(plugin_dir, exist_ok=True)

    plugins = []

    logo_tar_gz = tarfile.open(os.path.join(output_dir, "img.tar.gz"), "w:gz")

    for item in os.listdir(plugin_source_dir):
        plugin_path = os.path.join(plugin_source_dir, item)
        if os.path.isdir(plugin_path):
            print(f"Building plugin from '{plugin_path}'...")
            plugin = build_plugin(plugin_path, plugin_dir)
            add_file(logo_tar_gz, os.path.join(plugin_path, "logo.png"), f"./{plugin['name']}.png")
            plugins.append(plugin)
            print(f"Built plugin from '{plugin_path}' successfully.")
    
    logo_tar_gz.close()
    print("All plugins built successfully.")

    with open(os.path.join(output_dir, "plugins.json"), "w", encoding="utf-8") as f:
        json.dump(plugins, f, indent=4, ensure_ascii=False)

if __name__ == "__main__":
    main()