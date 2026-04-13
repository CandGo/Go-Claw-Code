---
domain: bilibili.com
aliases: [B站, 哔哩哔哩, bilibili, b站]
updated: 2026-04-11
---

## 平台特征
- SPA 架构，视频播放器使用 WebPlayer（JavaScript 渲染）
- 视频信息（标题、简介、分P列表）在页面 JS 变量 `window.__playinfo__` 和 `window.initialState` 中
- 弹幕是独立 API 加载的（`comment.bilibili.com`）
- 评论区是懒加载的，需要滚动触发

## 有效模式
- 视频页面：`https://www.bilibili.com/video/BVXXXXXXX` 直接 CDP 导航
- 视频信息：browser_eval `JSON.stringify({title: document.querySelector('h1')?.innerText, desc: document.querySelector('.desc-info-text')?.innerText})`
- 视频帧截图：browser_eval 操控 video 元素（获取 currentTime、seek、pause），然后 browser_screenshot
- 分P列表：browser_eval `document.querySelectorAll('.video-episode-card__info-title')` 或从 `window.initialState` 获取
- 搜索：`https://search.bilibili.com/all?keyword=KEYWORD`
- 评论区：browser_scroll 到评论区域后，browser_eval 获取 `.reply-content` 元素
- 用户主页：`https://space.bilibili.com/UID`

## 已知陷阱
- 视频播放器是 Shadow DOM 嵌套，CSS 选择器无法直接穿透，需 browser_eval 递归遍历
- 视频帧截图需要先暂停播放，否则截图可能是模糊的运动帧
- 评论区按热度排序是默认，切换到按时间需要点击切换按钮
- 短时间打开大量视频页面可能触发风控
