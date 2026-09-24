import { useState } from 'react'
import { postCounterEvidence } from './api'

const POSE_OPTIONS = [
  [0, '恒等（不旋转不镜像）'],
  [1, '顺时针旋转 90°'],
  [2, '旋转 180°'],
  [3, '顺时针旋转 270°'],
  [4, '左右镜像'],
  [5, '上下镜像'],
  [6, '主对角线翻转'],
  [7, '副对角线翻转'],
]

const COUNTER_MAX_N = 96

// 最小遮挡反证复核面板：在已有红蓝叠图结论旁输入一个非规范候选
// （姿态、纵移、横移），由服务端重新解析原图并重跑精确配准后求解最小遮挡窗口。
// 任何输入错误、无效候选或计算失败都会清空已有复核证据（onEvidenceChange(null)）。
export default function CounterReview({ n, reference, recheck, evidence, onEvidenceChange }) {
  const [pose, setPose] = useState('0')
  const [dy, setDy] = useState('0')
  const [dx, setDx] = useState('0')
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  const unsupported = n > COUNTER_MAX_N

  // 编辑候选即视为放弃上一次复核证据，避免旧窗口与新输入并存。
  const touch = () => {
    if (evidence) onEvidenceChange(null)
    if (error) setError(null)
  }

  const submit = async () => {
    setLoading(true)
    setError(null)
    onEvidenceChange(null) // 新复核发起即清空旧证据，失败时绝不残留

    const poseNum = Number(pose)
    const dyNum = Number(dy)
    const dxNum = Number(dx)
    const fail = (message, field = '') => {
      setError({ field, message })
      setLoading(false)
    }
    if (!/^\s*-?\d+\s*$/.test(pose) || !Number.isInteger(poseNum) || poseNum < 0 || poseNum > 7) {
      return fail('目标姿态须为 0..7 的整数', 'targetPose')
    }
    if (!/^\s*-?\d+\s*$/.test(dy) || !Number.isInteger(dyNum) ||
      dyNum < -(n - 1) || dyNum > n - 1) {
      return fail(`纵移须为 [${-(n - 1)}, ${n - 1}] 内的整数`, 'targetDy')
    }
    if (!/^\s*-?\d+\s*$/.test(dx) || !Number.isInteger(dxNum) ||
      dxNum < -(n - 1) || dxNum > n - 1) {
      return fail(`横移须为 [${-(n - 1)}, ${n - 1}] 内的整数`, 'targetDx')
    }

    try {
      const data = await postCounterEvidence(reference, recheck, poseNum, dyNum, dxNum)
      onEvidenceChange(data)
    } catch (err) {
      onEvidenceChange(null)
      setError({
        field: err.field || '',
        line: err.line || 0,
        column: err.column || 0,
        message: err.message || '复核失败',
      })
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="counter-panel">
      <h3>最小遮挡反证复核</h3>
      <p className="hint">
        怀疑规范姿态只是因局部遮挡偶然胜出时，输入一个非规范候选；服务端将重新解析两幅原图、
        重跑精确配准，并在权值（-1/0/1）网格上精确求面积最小的反证窗口（含负权，非贪心滑窗）。
      </p>

      {unsupported ? (
        <p className="counter-unsupported">
          当前边长 N={n}，反证复核仅支持边长 16..{COUNTER_MAX_N}；主审计结论不受影响。
        </p>
      ) : (
        <>
          <div className="counter-form">
            <label className="counter-field">
              <span>非规范姿态</span>
              <select value={pose} onChange={(e) => { setPose(e.target.value); touch() }}>
                {POSE_OPTIONS.map(([v, label]) => (
                  <option key={v} value={v}>{v} · {label}</option>
                ))}
              </select>
            </label>
            <label className="counter-field">
              <span>纵移 dy</span>
              <input
                type="number"
                step="1"
                value={dy}
                spellCheck={false}
                onChange={(e) => { setDy(e.target.value); touch() }}
              />
            </label>
            <label className="counter-field">
              <span>横移 dx</span>
              <input
                type="number"
                step="1"
                value={dx}
                spellCheck={false}
                onChange={(e) => { setDx(e.target.value); touch() }}
              />
            </label>
            <button className="primary counter-submit" onClick={submit} disabled={loading}>
              {loading ? '复核中…' : '发起反证复核'}
            </button>
          </div>

          {error && (
            <div className="counter-error" role="alert">
              {error.field && <span className="err-field">{FIELD_NAME[error.field] || error.field}</span>}
              {error.line > 0 && <span>第 {error.line} 行</span>}
              {error.column > 0 && <span>第 {error.column} 列</span>}
              <span>{error.message}</span>
              <span className="err-hint">复核证据已清空。</span>
            </div>
          )}

          {evidence && <CounterVerdict evidence={evidence} />}
        </>
      )}
    </div>
  )
}

const FIELD_NAME = {
  targetPose: '目标姿态',
  targetDy: '目标纵移',
  targetDx: '目标横移',
  reference: '参考图',
  recheck: '复检图',
}

function CounterVerdict({ evidence }) {
  const { canonical, target, gap, window: win } = evidence
  if (!evidence.feasible || !win) {
    return (
      <div className="counter-result counter-infeasible">
        <strong>不可反证。</strong>
        <span>
          不存在任何轴对齐非空矩形，移除其中参考缺陷后能使目标候选严格胜出；
          服务端未伪造窗口。
        </span>
        <OverlapTable canonical={canonical} target={target} gap={gap} />
      </div>
    )
  }
  return (
    <div className="counter-result counter-feasible">
      <strong>存在最小遮挡反证窗口（已叠绘在左图，琥珀色虚线框）。</strong>
      <ul className="counter-meta">
        <li>
          窗口坐标：行 {win.top}..{win.bottom}，列 {win.left}..{win.right}
          （{win.height} × {win.width}，面积 {win.area}）
        </li>
        <li>窗内参考缺陷 {win.removedPoints.length} 个：规范命中 {win.removedCanonical}、目标命中 {win.removedTarget}</li>
      </ul>
      <table className="counter-table">
        <thead>
          <tr><th>候选</th><th>姿态</th><th>dy</th><th>dx</th><th>移除前重合</th><th>移除后重合</th></tr>
        </thead>
        <tbody>
          <tr>
            <td>规范候选</td>
            <td>{canonical.poseLabel}</td>
            <td>{canonical.dy}</td>
            <td>{canonical.dx}</td>
            <td>{canonical.overlap}</td>
            <td>{win.canonicalOverlapAfter}</td>
          </tr>
          <tr>
            <td>目标候选</td>
            <td>{target.poseLabel}</td>
            <td>{target.dy}</td>
            <td>{target.dx}</td>
            <td>{target.overlap}</td>
            <td><strong>{win.targetOverlapAfter}</strong></td>
          </tr>
        </tbody>
      </table>
      <p className="hint">
        移除后目标候选 {win.targetOverlapAfter} &gt; 规范候选 {win.canonicalOverlapAfter}，目标严格胜出。
      </p>
    </div>
  )
}

function OverlapTable({ canonical, target, gap }) {
  return (
    <table className="counter-table">
      <thead>
        <tr><th>候选</th><th>姿态</th><th>dy</th><th>dx</th><th>重合数</th></tr>
      </thead>
      <tbody>
        <tr>
          <td>规范候选</td><td>{canonical.poseLabel}</td>
          <td>{canonical.dy}</td><td>{canonical.dx}</td><td>{canonical.overlap}</td>
        </tr>
        <tr>
          <td>目标候选</td><td>{target.poseLabel}</td>
          <td>{target.dy}</td><td>{target.dx}</td><td>{target.overlap}</td>
        </tr>
      </tbody>
      <tfoot>
        <tr><td colSpan="5">规范候选领先 {gap} 点，全图权值不足以反超。</td></tr>
      </tfoot>
    </table>
  )
}
