/* 桌面壳判定：桌面窗口 URL 带 ?desktop=1（TitleBar/看板入口等据此适配） */
export const isDesktop = new URLSearchParams(location.search).has('desktop')
