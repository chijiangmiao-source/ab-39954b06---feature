import { useState } from 'react'
import { postRefute } from './api'

// 与后端 D4 固定顺序一致的姿态选项（下标即裁决顺序）。
const POSE_OPTIONS = [
  { value: 0, label: '0 · 恒等' },
  { value: 1, label: '1 · 顺时针 90°' },
  { value: 2, label: '2 · 旋转 180°' },
  { value: 3, label: '3 · 顺时针 270°' },
  { value: 4, label: '4 · 左右镜像' },
  { value: 5, label: '5 · 上下镜像' },
  { value: 6, label: '6 · 主对角线翻转' },
  { value: 7, label: '7 · 副对角线翻转' },
]

// parseIntStrict 只接受十进制整数字符串（拒绝 "1.5"、"1e2"、""、"01 " 等）。
function parseIntStrict(raw) {
  const s = String(raw).trim()
  if (!/^-?\d+$/.test(s)) return null
  return Number(s)
}

// RefutePanel 是叠图结论旁的最小遮挡反证复核入口。
// 每次提交都把两幅原图原文上送，由服务端重新解析与精确配准。
export default function RefutePanel({ n, reference, recheck, onResult }) {
  const [pose, setPose] = useState(1)
  const [dyText, setDyText] = useState('0')
  const [dxText, setDxText] = useState('0')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const submit = async () => {
    // 前端先做一道输入校验；任何错误都立即清空既有复核证据。
    const dy = parseIntStrict(dyText)
    const dx = parseIntStrict(dxText)
    if (dy === null) {
      setError({ field: 'dy', message: '纵移必须是整数' })
      onResult(null)
      return
    }
    if (dx === null) {
      setError({ field: 'dx', message: '横移必须是整数' })
      onResult(null)
      return
    }
    if (dy < -(n - 1) || dy > n - 1) {
      setError({ field: 'dy', message: `纵移需在 [${-(n - 1)}, ${n - 1}] 内（边长 N=${n}）` })
      onResult(null)
      return
    }
    if (dx < -(n - 1) || dx > n - 1) {
      setError({ field: 'dx', message: `横移需在 [${-(n - 1)}, ${n - 1}] 内（边长 N=${n}）` })
      onResult(null)
      return
    }

    setLoading(true)
    setError(null)
    onResult(null) // 新请求发出即清空旧证据，失败时绝不残留
    try {
      const data = await postRefute(reference, recheck, pose, dy, dx)
      onResult(data)
    } catch (err) {
      setError({
        field: err.field || '',
        line: err.line || 0,
        column: err.column || 0,
        message: err.message || '复核失败',
      })
      onResult(null)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="refute-card">
      <h3>最小遮挡反证复核</h3>
      <p className="hint">
        怀疑规范姿态只是偶然胜出时，输入一个候选姿态与纵移、横移；
        服务端将重新解析两幅原图并重跑精确配准，在全部轴对齐非空矩形中
        精确求面积最小的遮挡窗口（含负权，非贪心滑窗）。仅支持边长 16–96。
      </p>
      <div className="refute-form">
        <label className="inline">
          候选姿态
          <select value={pose} onChange={(e) => setPose(Number(e.target.value))}>
            {POSE_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
        </label>
        <label className={`inline num-field ${error && error.field === 'dy' ? 'num-bad' : ''}`}>
          纵移 dy
          <input
            type="text"
            inputMode="numeric"
            spellCheck={false}
            value={dyText}
            onChange={(e) => setDyText(e.target.value)}
          />
        </label>
        <label className={`inline num-field ${error && error.field === 'dx' ? 'num-bad' : ''}`}>
          横移 dx
          <input
            type="text"
            inputMode="numeric"
            spellCheck={false}
            value={dxText}
            onChange={(e) => setDxText(e.target.value)}
          />
        </label>
        <button className="primary" onClick={submit} disabled={loading || n > 96}>
          {loading ? '复核中…' : '发起反证复核'}
        </button>
      </div>
      {n > 96 && (
        <p className="refute-note">当前边长 N={n}，反证复核入口仅支持 16–96；主审计结论不受影响。</p>
      )}
      {error && (
        <p className="refute-error" role="alert">
          {error.field && <span className="err-field">{fieldLabel(error.field)} </span>}
          {error.line > 0 && <span>第 {error.line} 行 </span>}
          {error.column > 0 && <span>第 {error.column} 列 </span>}
          <span className="err-msg">{error.message}</span>
          <span className="err-hint">复核证据已清空。</span>
        </p>
      )}
    </div>
  )
}

function fieldLabel(f) {
  return (
    {
      poseIndex: '候选姿态',
      dy: '纵移',
      dx: '横移',
      reference: '参考图',
      recheck: '复检图',
    }[f] || f
  )
}
