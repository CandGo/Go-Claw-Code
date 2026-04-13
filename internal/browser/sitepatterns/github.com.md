---
domain: github.com
aliases: [GitHub, github]
updated: 2026-04-11
---

## 平台特征
- SPA 架构，大量内容通过 JS 动态渲染，纯 WebFetch 难以获取完整内容
- 代码文件内容在 `<textarea>` 或 `<pre>` 标签中，可通过 browser_eval 直接读取
- PR / Issue 页面的评论是动态加载的
- 登录用户有更高的 API rate limit

## 有效模式
- 代码文件：browser_eval `document.querySelector('#read-only-cursor-text-area')?.value || document.querySelector('pre')?.textContent`
- README 内容：browser_eval `document.querySelector('article')?.innerText`
- PR diff：browser_eval 获取 `.js-file-content` 或 diff 行
- 文件树：browser_eval 递归获取 `.js-navigation-item` 元素
- 搜索结果：`https://github.com/search?q=KEYWORD&type=code`
- Raw 文件：`https://raw.githubusercontent.com/OWNER/REPO/BRANCH/PATH`（公开仓库可用 WebFetch 直接获取）
- API：`https://api.github.com/repos/OWNER/REPO`（公开 API，无需 token，rate limit 60/h）

## 已知陷阱
- 未登录时部分私有仓库和搜索功能受限
- 文件内容可能被截断（超长文件需要滚动加载）
- 短时间内大量请求可能触发 rate limit（返回 429）
