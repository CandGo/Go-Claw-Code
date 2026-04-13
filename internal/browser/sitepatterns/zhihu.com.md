---
domain: zhihu.com
aliases: [知乎, zhihu]
updated: 2026-04-11
---

## 平台特征
- SPA 架构（React），内容动态渲染，WebFetch 无法获取完整内容
- 需要登录才能查看大部分内容（回答、文章全文）
- 反爬机制严格：频率限制、JS 指纹检测
- 移动端 API（api.zhihu.com）返回 JSON，但需要有效的 authorization token

## 有效模式
- 问题页面：`https://www.zhihu.com/question/ID` 直接 CDP 导航
- 回答内容：browser_eval 获取 `.RichContent-inner` 的 innerText
- 文章：`https://zhuanlan.zhihu.com/p/ID`
- 文章内容：browser_eval `document.querySelector('.Post-RichText')?.innerText`
- 搜索：`https://www.zhihu.com/search?q=KEYWORD&type=content`
- 搜索结果：browser_eval 获取 `.SearchResult-Card` 元素

## 已知陷阱
- 未登录时大部分回答内容被折叠或截断，必须使用 CDP 并利用用户登录态
- 频繁请求会触发验证码或临时封禁
- 搜索结果可能因为登录状态不同而差异很大
- 回答排序默认是"默认排序"而非"按时间排序"，需要注意切换
