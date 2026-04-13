---
domain: bing.com
aliases: [Bing, 必应]
updated: 2026-04-11
---

## 平台特征
- 搜索结果页是 SSR + 部分动态渲染
- 相比百度，广告标记更清晰
- 国际版和国内版内容差异大（cn.bing.com vs www.bing.com）
- 图片搜索使用瀑布流加载

## 有效模式
- 搜索：`https://www.bing.com/search?q=KEYWORD` 或 `https://cn.bing.com/search?q=KEYWORD`
- 搜索结果：browser_eval 获取 `#b_results > li.b_algo` 的标题和摘要
- 图片搜索：`https://www.bing.com/images/search?q=KEYWORD`
- 图片 URL：browser_eval 获取 `.iusc` 元素的 `m` 属性（JSON 格式含图片 URL）
- 新闻：`https://www.bing.com/news/search?q=KEYWORD`

## 已知陷阱
- 国际版 www.bing.com 可能被重定向到国内版
- 图片搜索结果通过 JS 动态加载，需要滚动触发懒加载
- 部分搜索结果使用 AMP 缓存，URL 格式不同
