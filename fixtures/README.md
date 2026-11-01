# 合成测试数据

此目录的科研标题、摘要、简报、雷达和条目 ID 全部为本项目人工编写的合成示例，按项目 MIT 许可提供；不对应真实论文，不是 UrbanComp 在线平台的完整数据副本。`example.org` 来源地址仅用于测试合法 URL 格式，不应期待它提供对应论文。

运行 `python3 fixtures/generate.py` 可确定性重建两个 JSON 文件，不访问网络。

- `synthetic-dashboard.json`：6 篇示例、2 个雷达栏目、1 个简报；区分发布与收录时间，包含只有卷期信息的条目。
- `synthetic-dashboard-with-interests.json`：30 篇候选、重复条目与非论文条目；用于 24 篇选文上限及逐页渲染测试，包含长正文。

历史日期用于固定测试，不代表数据新鲜度。真正的设备续航证据在 `docs/validation/26h/`，没有用这些示例替换实测性能记录。README 的实机截图仍保留当时的界面，用作项目展示。
