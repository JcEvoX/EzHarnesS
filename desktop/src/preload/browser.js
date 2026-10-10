/*
浏览器标签视图（WebContentsView）的 preload：只管 Ctrl+滚轮缩放。

抽屉里那块视图通常比桌面浏览器窗口窄，Ctrl+滚轮是用户调"看得多 / 看得清"的
直觉入口。Electron 没有内置的 ctrl+wheel→zoom（那是浏览器 UI 层的职责），所以
在页面 DOM 上捕获 wheel 转发给主进程去改 zoomFactor。事件监听挂在隔离世界，
但 DOM 事件是共享的、preventDefault 也作用于真实事件，页面侧不需要任何配合。
*/
const { ipcRenderer } = require('electron')

window.addEventListener(
  'wheel',
  (e) => {
    if (!e.ctrlKey) return
    e.preventDefault() // 别让页面自己缩放/滚动
    ipcRenderer.send('ez-browser:wheel-zoom', e.deltaY < 0 ? 'in' : 'out')
  },
  { passive: false, capture: true },
)
