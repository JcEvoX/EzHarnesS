/*
图片压缩：多模态输入前的体积控制——长边超过 2048 缩到 2048；
含透明像素输出 PNG（保透明），否则白底 JPEG（质量 0.85）；
原图未缩放且 ≤500KB 直接原样 base64（免二次损失）。
*/

export interface CompressedImage {
  mimeType: string
  data: string // 纯 base64（无 data: 前缀）
}

const MAX_EDGE = 2048
const RAW_LIMIT = 500 * 1024

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => {
      // FileReader 结果是 data URL，取逗号后的纯 base64
      resolve(String(r.result).split(',')[1] ?? '')
    }
    r.onerror = () => reject(r.error ?? new Error('read failed'))
    r.readAsDataURL(blob)
  })
}

function hasAlpha(ctx: CanvasRenderingContext2D, w: number, h: number): boolean {
  const data = ctx.getImageData(0, 0, w, h).data
  for (let i = 3; i < data.length; i += 4) {
    if (data[i] < 255) return true
  }
  return false
}

function canvasToBlob(canvas: HTMLCanvasElement, mime: string, q?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (b) => (b ? resolve(b) : reject(new Error('encode failed'))),
      mime,
      q,
    )
  })
}

export async function compressImage(file: File): Promise<CompressedImage> {
  const bitmap = await createImageBitmap(file)
  try {
    const scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height))
    if (scale === 1 && file.size <= RAW_LIMIT && (file.type === 'image/jpeg' || file.type === 'image/png')) {
      return { mimeType: file.type, data: await blobToBase64(file) }
    }
    const w = Math.max(1, Math.round(bitmap.width * scale))
    const h = Math.max(1, Math.round(bitmap.height * scale))
    const canvas = document.createElement('canvas')
    canvas.width = w
    canvas.height = h
    const ctx = canvas.getContext('2d', { willReadFrequently: true })
    if (!ctx) throw new Error('canvas unavailable')
    ctx.drawImage(bitmap, 0, 0, w, h)
    const png = file.type === 'image/png' || file.type === 'image/webp' || file.type === 'image/gif'
      ? hasAlpha(ctx, w, h)
      : false
    if (png) {
      const b = await canvasToBlob(canvas, 'image/png')
      return { mimeType: 'image/png', data: await blobToBase64(b) }
    }
    // 不透明图铺白底再 JPEG（canvas 本身黑底，直接编码会发暗）
    const white = document.createElement('canvas')
    white.width = w
    white.height = h
    const wctx = white.getContext('2d')
    if (!wctx) throw new Error('canvas unavailable')
    wctx.fillStyle = '#fff'
    wctx.fillRect(0, 0, w, h)
    wctx.drawImage(canvas, 0, 0)
    const b = await canvasToBlob(white, 'image/jpeg', 0.85)
    return { mimeType: 'image/jpeg', data: await blobToBase64(b) }
  } finally {
    bitmap.close()
  }
}
