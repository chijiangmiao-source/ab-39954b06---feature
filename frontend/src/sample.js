// 与后端一致的 D4 姿态映射，用于在浏览器侧生成示例与随机用例。
const POSES = [
  (r, c, n) => [r, c],
  (r, c, n) => [c, n - 1 - r],
  (r, c, n) => [n - 1 - r, n - 1 - c],
  (r, c, n) => [n - 1 - c, r],
  (r, c, n) => [r, n - 1 - c],
  (r, c, n) => [n - 1 - r, c],
  (r, c, n) => [c, r],
  (r, c, n) => [n - 1 - c, n - 1 - r],
]

function pointsToText(n, pts) {
  const grid = Array.from({ length: n }, () => new Array(n).fill('0'))
  for (const [r, c] of pts) grid[r][c] = '1'
  return grid.map((row) => row.join('')).join('\n')
}

// 固定示例：复检图 = 参考图顺时针旋转 90° 后纵移 +2、横移 -1，全部留在画布内。
export function makeSample() {
  const n = 16
  const refPts = [
    [2, 3], [3, 9], [4, 4], [6, 12], [7, 7], [9, 11],
    [11, 5], [12, 10], [13, 13], [5, 2], [10, 8], [8, 6],
  ]
  const pose = 1
  const dy = 2
  const dx = -1
  const recPts = refPts.map(([r, c]) => {
    const [pr, pc] = POSES[pose](r, c, n)
    return [pr + dy, pc + dx]
  })
  return { reference: pointsToText(n, refPts), recheck: pointsToText(n, recPts) }
}

// 随机用例：随机姿态 + 随机平移；移出画布的缺陷不会出现在复检图中（模拟真实视野偏移）。
export function makeRandom(n, density) {
  const refPts = []
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (Math.random() < density) refPts.push([r, c])
    }
  }
  const pose = Math.floor(Math.random() * 8)
  const span = Math.max(1, Math.floor(n / 4))
  const dy = Math.floor(Math.random() * (2 * span + 1)) - span
  const dx = Math.floor(Math.random() * (2 * span + 1)) - span
  const recPts = []
  for (const [r, c] of refPts) {
    const [pr, pc] = POSES[pose](r, c, n)
    const tr = pr + dy
    const tc = pc + dx
    if (tr >= 0 && tr < n && tc >= 0 && tc < n) recPts.push([tr, tc])
  }
  return { reference: pointsToText(n, refPts), recheck: pointsToText(n, recPts) }
}
