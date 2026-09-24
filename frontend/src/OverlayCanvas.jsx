import { useEffect, useRef } from 'react'

// 红蓝叠加证据图：参考图缺陷画蓝色，规范变换后的复检图缺陷画半透明红色，
// 重合处自然叠成紫色；复检图中被移出画布的点绘制在画布边界外的扩展区域。
//
// 可选 windowRect：最小遮挡反证复核求得的可行窗口（画布内格子坐标，含端点），
// 以半透明绿色填充 + 粗描边绘制；removed 中的被移除参考缺陷加金色描边。
export default function OverlayCanvas({ n, overlay, windowRect, removed }) {
  const ref = useRef(null)

  useEffect(() => {
    const canvas = ref.current
    if (!canvas) return
    const { matched, referenceOnly, recheckOnly } = overlay

    // 视野 = 原画布 ∪ 复检变换后所有点的包围盒（反证窗口恒在原画布内）
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

    // 最小遮挡反证窗口：半透明绿底 + 粗描边（先于原画布边界绘制，边界仍清晰）
    if (windowRect) {
      const [x, y] = px(windowRect.top, windowRect.left)
      const w = (windowRect.right - windowRect.left + 1) * cell
      const h = (windowRect.bottom - windowRect.top + 1) * cell
      ctx.fillStyle = 'rgba(21, 128, 61, 0.18)'
      ctx.fillRect(x, y, w, h)
      ctx.strokeStyle = '#15803d'
      ctx.lineWidth = Math.max(2, Math.round(cell / 4))
      ctx.strokeRect(x + 1, y + 1, w - 2, h - 2)

      // 被移除的参考缺陷：金色描边
      if (removed && removed.length) {
        ctx.strokeStyle = '#d97706'
        ctx.lineWidth = Math.max(1.5, Math.round(cell / 6))
        for (const [r, c] of removed) {
          const [rx, ry] = px(r, c)
          ctx.strokeRect(rx + 1, ry + 1, cell - 2, cell - 2)
        }
      }
    }

    // 原画布边界
    ctx.strokeStyle = '#0f172a'
    ctx.lineWidth = 2
    ctx.strokeRect(-minC * cell + 1, -minR * cell + 1, n * cell - 2, n * cell - 2)
  }, [n, overlay, windowRect, removed])

  return <canvas ref={ref} className="overlay-canvas" />
}
