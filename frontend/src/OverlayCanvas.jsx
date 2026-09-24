import { useEffect, useRef } from 'react'

// 红蓝叠加证据图：参考图缺陷画蓝色，规范变换后的复检图缺陷画半透明红色，
// 重合处自然叠成紫色；复检图中被移出画布的点绘制在画布边界外的扩展区域。
export default function OverlayCanvas({ n, overlay }) {
  const ref = useRef(null)

  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    const { matched, referenceOnly, recheckOnly } = overlay

    // 视野 = 原画布 ∪ 复检变换后所有点的包围盒
    let minR = 0
    let minC = 0
    let maxR = n - 1
    let maxC = n - 1
    for (const [r, c] of recheckOnly) {
      if (r < minR) minR = r
      if (c < minC) minC = c
      if (r > maxR) maxR = r
      if (c > maxC) maxC = c
    }
    const rows = maxR - minR + 1
    const cols = maxC - minC + 1
    const maxPix = 560
    const cell = Math.max(1, Math.floor(maxPix / Math.max(rows, cols)))

    canvas.width = cols * cell
    canvas.height = rows * cell
    const ctx = canvas.getContext('2d')

    // 画布外区域底色 + 原画布区域底色
    ctx.fillStyle = '#e8edf5'
    ctx.fillRect(0, 0, canvas.width, canvas.height)
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(-minC * cell, -minR * cell, n * cell, n * cell)

    // 网格线（格子足够大时）
    if (cell >= 6) {
      ctx.strokeStyle = '#e2e8f0'
      ctx.lineWidth = 1
      for (let i = 0; i <= cols; i++) {
        ctx.beginPath()
        ctx.moveTo(i * cell + 0.5, 0)
        ctx.lineTo(i * cell + 0.5, canvas.height)
        ctx.stroke()
      }
      for (let i = 0; i <= rows; i++) {
        ctx.beginPath()
        ctx.moveTo(0, i * cell + 0.5)
        ctx.lineTo(canvas.width, i * cell + 0.5)
        ctx.stroke()
      }
    }

    const px = (r, c) => [(c - minC) * cell, (r - minR) * cell]

    // 参考图：蓝色
    ctx.globalAlpha = 0.9
    ctx.fillStyle = '#1d4ed8'
    for (const [r, c] of matched) {
      const [x, y] = px(r, c)
      ctx.fillRect(x, y, cell, cell)
    }
    for (const [r, c] of referenceOnly) {
      const [x, y] = px(r, c)
      ctx.fillRect(x, y, cell, cell)
    }

    // 复检图（规范变换后）：半透明红色，叠在蓝点上呈紫色
    ctx.globalAlpha = 0.62
    ctx.fillStyle = '#dc2626'
    for (const [r, c] of matched) {
      const [x, y] = px(r, c)
      ctx.fillRect(x, y, cell, cell)
    }
    for (const [r, c] of recheckOnly) {
      const [x, y] = px(r, c)
      ctx.fillRect(x, y, cell, cell)
    }
    ctx.globalAlpha = 1

    // 原画布边界
    ctx.strokeStyle = '#0f172a'
    ctx.lineWidth = 2
    ctx.strokeRect(-minC * cell + 1, -minR * cell + 1, n * cell - 2, n * cell - 2)
  }, [n, overlay])

  return <canvas ref={ref} className="overlay-canvas" />
}
