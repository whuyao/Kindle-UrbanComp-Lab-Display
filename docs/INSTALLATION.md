# 安装、更新与恢复

## 已验证的前提

本流程针对 Kindle Paperwhite 2（4 GB）、固件 **5.12.2.2**、ARMv7 / Linux 3.0.35。设备已完成越狱，书库可通过现有 KMC 环境以 root 运行 `.sh` 脚本；FBInk 位于 `/var/local/kmc/bin/fbink`，相关库位于 `/var/local/kmc/lib`。这些组件不在本安装包中。

项目起步时使用了 WinterBreak；这里只归档后续客户端改造，不提供通用越狱教程。其他型号、固件和启动器未经本项目验证。不要删除脚本里的固件、架构和驱动检查来尝试安装。

安装会新增 `/etc/upstart/urbancomp-ink.conf` 并备份本项目原有启动配置，**不替换完整固件**。`backups/` 不是全盘恢复镜像。电量须高于 30%，连接可用 Wi-Fi。下述 Kindle 脚本只能在设备启动器中运行，不要在电脑上执行。

## 首次安装

1. 按 [开发文档](DEVELOPMENT.md) 构建安装包，解压 `dist/Kindle-UrbanComp-Lab-Display-pw2.zip`。
2. USB 连接 Kindle，将包内 `urbancomp-ink/` 复制到 Kindle 磁盘根目录；把 `documents/` 内的脚本加入现有 `documents/`，保留原有书籍。
3. 从电脑安全弹出 Kindle 并拔线。打开书库中的 `UrbanComp-Ink-Check`，约半分钟后接回 USB，检查 `urbancomp-ink-check/report.txt`。它收集版本、启动服务、屏幕和 HTTPS 能力，不安装服务。不要把整份设备报告直接提交 GitHub。
4. 弹出、拔线，保持 Wi-Fi 在线，打开 `UrbanComp-Ink-Native-Test`。等待约 1–2 分钟，确认确实出现科研画面。接回 USB 检查 `urbancomp-ink/tested-driver.txt` 为 `fbink`、`state/frame.png` 存在及测试日志无错误。脚本成功退出本身不能代替屏幕目视确认。
5. 再次弹出、拔线，打开 `UrbanComp-Ink-Install`。它校验安装包，测试全屏接管与退出恢复，通过后自动重启一次。约 2–3 分钟后应自动进入“知新”。安装期间等待完成。
6. 验证科研画面稳定、右下角“主页”能恢复书库，再打开 `UrbanComp-Ink-Start`；确认标题进入详情、翻页、返回与继续阅读正常。
7. 按 [硬件验收清单](TESTING.md) 检查半小时更新、断网缓存和长时运行。

## 日常使用

- 每 30 分钟检查科研与天气接口；栏目时间来自源数据，接口没有更新时显示昨天的栏目是正常的。
- 程序直接联网，Mac 可以关机。开启飞行模式后只能显示缓存。
- 首页电量是最近一次采样并随画面更新的数值，不是每秒刷新的电量表。
- 空闲两分钟关闭前光，触摸恢复原亮度。右下角“主页”退出常驻并恢复系统界面；点击文章可进入阅读。
- 电量低至 20%、进入休眠或 USB 存储模式会退出常驻；充电后可用 Start 再进入。

## 更新现有安装

1. 先返回主页或连接 USB，让常驻程序退出。备份自己的 `urbancomp-ink/state/` 和 `backups/` 到电脑。
2. 构建新安装包，覆盖包内同名文件。保留设备的 `state/`、`backups/`、`tested-driver.txt`，不要用整个空文件夹替换设备目录。
3. 安全弹出、拔线，打开 `UrbanComp-Ink-Update`。它调用同一安装检查和恢复探测，通过后直接启动，不要求重启。
4. 如仅需继续运行已有版本，使用 `UrbanComp-Ink-Start` 即可。更新脚本失败时先读取 `install.log`，不要反复强制启动。

## 禁用、恢复与卸载

- **禁用开机接管：**通过 USB 在 `urbancomp-ink/` 中新建空文件 `DISABLED`，或将文件夹改名为 `urbancomp-ink.off`；随后正常重启。
- **恢复进入科研屏：**目录恢复原名，在电量足够且拔线后打开 Start。Start 会移除禁用标记。
- **卸载启动服务：**在书库打开 `UrbanComp-Ink-Remove`，仅清理本项目拥有的启动服务并恢复相关状态，保留缓存、日志和备份，不清空书籍。
- **意外退出：**查看 `state/last-exit.json`、`state/supervisor-events.jsonl`、`state/session.log` 和最近三份轮转日志。低电量退出属于预期行为。

USB 接管时无法写入用户盘的退出记录暂存在内存，下次 Start 再落盘；期间重启会丢失尚未保存的记录。读取日志前尽量不要无故重启。
