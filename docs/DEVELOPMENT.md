# 开发与构建

所有命令从仓库根目录执行。需要 Python 3.10+、**Go 1.23.12** 和首次下载依赖所需的网络。Go 版本为已验证旧内核设备而固定；不要无验证地升级编译器。字体已随源码保存，保留对应授权文件。

## 构建与自动化测试

```sh
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements.txt
python -m unittest discover -s tests -v
python -m unittest discover -s native/tests -v
python native/build.py
(cd native/app && go test -race ./...)
```

如果 Go 不在 PATH，用 `INK_GO=/absolute/path/to/go python native/build.py`。构建默认使用仓库 `.cache/`，也接受已有 `GOCACHE`、`GOPATH`；代理使用标准 `HTTPS_PROXY` 等环境变量，不把凭据写进仓库。

构建脚本先运行 Go 测试，再输出：

- `dist/ink-host`：本机预览程序。
- `native/package/urbancomp-ink/ink-native`：`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7` 的静态设备程序。
- `native/package/urbancomp-ink/SHA256SUMS`：本次安装文件校验清单。
- `dist/Kindle-UrbanComp-Lab-Display-pw2.zip`：可复制到 Kindle 的目录结构，包括预检查与书库脚本。

构建不连接或写入 Kindle；产物不提交 Git。打包采用文件白名单，不会把本地缓存、日志或阅读进度装入发布包。`.github/workflows/test.yml` 在干净的 macOS 环境执行同样的两组 Python 测试、原生构建、Go 竞态检测和实测数据分析，成功后保存安装包 artifact。Go 主机测试采用 macOS，因为应用在 Linux 上会强制检查真实 `/mnt/us` 挂载；交叉构建仍面向 Linux ARMv7，不为 CI 绕过设备保护。

## 桌面原型预览

```sh
python -m ink build --input fixtures/synthetic-dashboard.json
python -m ink validate generated/pw2
python -m ink serve --host 127.0.0.1 --port 8765
```

浏览器打开 `http://127.0.0.1:8765/`。`Start-Ink.command` 是同一服务的快捷入口，默认仅本机访问；需要局域网访问时自行设置 `INK_HOST` 为本机网络地址。服务器模式会访问公开上游，离线排版用 `build --input`。

Python 原型现在默认使用仓库里的思源字体，不依赖 macOS 专有字体；历史截图可能使用此前字体。可通过 `INK_FONT_SANS`、`INK_FONT_BOLD`、`INK_FONT_SERIF`、`INK_FONT_LATIN` 及相应 `_INDEX` 变量覆盖。

可选桌面浏览器模拟需要额外安装 Playwright 及 Chromium。在预览服务已运行时执行 `node tests/browser.cjs`，支持 `INK_URL`、`INK_PLAYWRIGHT_PATH`、`INK_CHROME`。它只验证桌面浏览器原型，不证明 Kindle 硬件兼容性，也不在默认 CI 中运行。

## 原生排版预览

`dist/ink-host -help` 列出选项。`-once -no-display` 执行一次取数与排版；增加 `-fixture fixtures/synthetic-dashboard.json` 后使用科研快照、跳过网络取数（不会生成新的天气）。传入的 `-dir` 需有 `fonts/`，运行后会产生本地 `state/`。

有兼容的本地 `state/content.json` 后，使用：

```sh
dist/ink-host -dir /path/to/local-preview-data -preview /tmp/ink-home.png
dist/ink-host -dir /path/to/local-preview-data -preview /tmp/ink-paper.png -preview-detail paper:0
```

预览目录还需有 `fonts/`；`-preview` 不联网、不写设备屏幕。不要将个人阅读进度和原始设备日志加入 Git。原生测试使用仓库合成示例和字体，可以离线检查布局与交互逻辑。

## 修改后的最小验证

排版或交互修改运行 Go 测试并目视检查对应页面；监管或恢复修改必须运行 `native/tests`，再做设备退出、USB 和恢复验证。构建成功只说明软件检查通过，不能代替安装探测与硬件目视验收。
