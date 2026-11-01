# 第三方素材、数据与权利说明

项目原创代码、文档与合成测试数据按根目录 [LICENSE](LICENSE) 的 MIT 许可提供。下列品牌标志、截图中的第三方内容、字体与证书不包含在这一 MIT 授权中，分别适用本文件列出的权利范围和原有许可。

## 实验室标志

`native/app/assets/urbancomp-logo.png` 来自实验室提供的 UrbanComp 标志。标志、名称与品牌权利归相应权利人；本项目用于实验室设备显示。README 截图包含该标志。标志和 UrbanComp 名称不按 MIT 许可授权，也不授予商标权或实验室背书；分发衍生产品时请换用自己的品牌，其他使用需取得相应权利人的许可。

## 字体

思源黑体与思源宋体来自 Adobe Source Han 项目，按随附 SIL Open Font License 分发：

- [Source Han Sans](https://github.com/adobe-fonts/source-han-sans)：`native/package/urbancomp-ink/fonts/LICENSE-sans.txt`。
- [Source Han Serif](https://github.com/adobe-fonts/source-han-serif)：`native/package/urbancomp-ink/fonts/LICENSE-serif.txt`。

文件来源 URL 与 SHA-256 保存在 `native/package/urbancomp-ink/ASSETS.json`。字体二进制保持原样，授权文件随包分发。

## CA 与 Go 依赖

`cacert.pem` 为 curl 发布、从 Mozilla 根证书库提取的公共 CA 集合，来源与快照日期保留在文件头，下载地址记录在 ASSETS.json。相关说明见 [curl CA Extract](https://curl.se/docs/caextract.html)。该转换文件沿用 Mozilla 源文件的 MPL 2.0 许可，副本在 `native/package/urbancomp-ink/licenses/CA-MPL-2.0.txt`；对应源文件 URL 保留在证书文件头。证书不是设备私钥或账户凭据。

Go 模块版本和完整性校验见 `native/app/go.mod`、`go.sum`。`golang.org/x/image` 和 `golang.org/x/text` 使用各自的 BSD 风格许可，构建依赖从模块代理下载，不将其源码伪称为本项目原创。Go 标准库及运行时使用 Go 项目许可。安装包中的依赖许可由构建流程另附。

## 设备组件与内容

FBInk、KMC、Kindle 固件和越狱组件均为外部依赖，不在本仓库中重新分发二进制。

科研数据来自 UrbanComp 的公开接口，论文标题、摘要及来源链接的权利归原作者或相应平台。天气来自 Open-Meteo，保留页面来源归属；相关数据使用条件以服务方为准。仓库的 fixtures 已改为本项目原创的合成示例，不再包含完整上游科研快照。截图仅记录布局与当时的展示结果，内含第三方标题、摘要及链接不因此获得 MIT 再授权。

## 数据边界

仓库没有上传设备序列号、Amazon 账户、Wi-Fi 凭据、完整设备诊断、个人阅读历史或原始备份。实测性能文件只筛选必要会话并替换标识。今后提交前仍需检查新文件，尤其不要直接添加设备的 `state/`、`backups/` 或整个 USB 磁盘。
