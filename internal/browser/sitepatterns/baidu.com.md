---
domain: baidu.com
aliases: [百度, baidu]
updated: 2026-04-11
---

## 平台特征
- 搜索结果页是 SPA + SSR 混合架构，主要内容通过 JS 渲染
- 搜索结果包含大量广告和推广内容，需区分自然结果和推广
- 百度百科、百度知道等子站点各有不同结构
- 反爬机制：频繁请求可能触发验证码

## 有效模式
- 搜索：`https://www.baidu.com/s?wd=KEYWORD` 直接导航
- 自然搜索结果：browser_eval 获取 `.result.c-container` 元素的标题和摘要
- 百度百科词条内容：browser_eval `document.querySelector('.main-content')?.innerText`
- 百度知道问答：browser_eval 获取 `.best-answer` 或 `.answer-txt`
- 百度学术：`https://xueshu.baidu.com/s?wd=KEYWORD`

## 已知陷阱
- 搜索结果顶部和侧边有广告，选择器需排除广告位（`.ec_wise_ad` 等）
- 部分搜索结果需要点击才能展开完整内容
- 百科词条可能有多个版本（中文、英文），需注意切换
- 图片搜索结果不能直接通过 URL 获取原始图片，需要解析跳转链接
