import { useCallback, useEffect, useState } from 'react'
import { fetchHealth, postAudit } from './api'
import OverlayCanvas from './OverlayCanvas'
import { makeRandom, makeSample } from './sample'

const FIELD_LABEL = { reference: '参考图', recheck: '复检图' }

function lineCount(text) {
  return text.trim() === '' ? 0 : text.trim().split('\n').length
}

export default function App() {
  const [refText, setRefText] = useState('')
  const [recText, setRecText] = useState('')
  const [result, setResult] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [health, setHealth] = useState('checking') // checking | ok | down
  const [randN, setRandN] = useState(32)
  const [density, setDensity] = useState(0.05)

  useEffect(() => {
    let cancelled = false
    const ping = () =>
      fetchHealth()
        .then(() => !cancelled && setHealth('ok'))
        .catch(() => !cancelled && setHealth('down'))
    ping()
    const timer = setInterval(ping, 15000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [])

  const submit = useCallback(async () => {
    setLoading(true)
    setError(null)
    setResult(null) // 新请求发出即清空旧结论，异常时绝不残留
    try {
      const data = await postAudit(refText, recText)
      setResult(data)
    } catch (err) {
      setError({
        field: err.field || '',
        line: err.line || 0,
        column: err.column || 0,
        message: err.message || '未知错误',
      })
    } finally {
      setLoading(false)
    }
  }, [refText, recText])

  const loadSample = () => {
    const s = makeSample()
    setRefText(s.reference)
    setRecText(s.recheck)
    setError(null)
  }

  const loadRandom = () => {
    const s = makeRandom(randN, density)
    setRefText(s.reference)
    setRecText(s.recheck)
    setError(null)
  }

  const clearAll = () => {
    setRefText('')
    setRecText('')
    setError(null)
    setResult(null)
  }

  return (
    <div className="page">
      <header className="header">
        <div>
          <h1>晶圆缺陷复检审计台</h1>
          <p className="subtitle">
            穷举 8 种旋转/镜像 × 横纵 -(N-1)..(N-1) 整数平移，精确整数二维相关求最大重合
          </p>
        </div>
        <span className={`badge badge-${health}`}>
          {health === 'ok' ? '后端在线' : health === 'down' ? '后端不可达' : '检测中…'}
        </span>
      </header>

      <section className="inputs">
        <MatrixInput
          id="reference"
          label="参考图（首次记录）"
          value={refText}
          onChange={setRefText}
          invalid={error && error.field === 'reference'}
        />
        <MatrixInput
          id="recheck"
          label="复检图（装片偏移后）"
          value={recText}
          onChange={setRecText}
          invalid={error && error.field === 'recheck'}
        />
      </section>

      <section className="toolbar">
        <button className="primary" onClick={submit} disabled={loading || health !== 'ok'}>
          {loading ? '审计中…' : '开始审计'}
        </button>
        <button onClick={loadSample}>载入示例</button>
        <label className="inline">
          边长
          <select value={randN} onChange={(e) => setRandN(Number(e.target.value))}>
            {[16, 32, 64, 128, 256, 512].map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
        </label>
        <label className="inline">
          密度
          <select value={density} onChange={(e) => setDensity(Number(e.target.value))}>
            {[0.02, 0.05, 0.1, 0.3, 1].map((v) => (
              <option key={v} value={v}>{v}</option>
            ))}
          </select>
        </label>
        <button onClick={loadRandom}>随机生成</button>
        <button onClick={clearAll}>清空</button>
      </section>

      {error && (
        <section className="error-banner" role="alert">
          <strong>输入有误，已定位：</strong>
          {error.field && <span className="err-field">{FIELD_LABEL[error.field] || error.field}</span>}
          {error.line > 0 && <span>第 {error.line} 行</span>}
          {error.column > 0 && <span>第 {error.column} 列</span>}
          <span className="err-msg">{error.message}</span>
          <span className="err-hint">修正后可直接重试，旧结论已清除。</span>
        </section>
      )}

      {result && (
        <section className="result">
          <h2>审计结论</h2>
          <div className="stats">
            <Stat label="边长 N" value={result.n} />
            <Stat label="参考图缺陷数" value={result.referenceCount} />
            <Stat label="复检图缺陷数" value={result.recheckCount} hint="含移出画布者" />
            <Stat label="最大重合数" value={result.maxOverlap} highlight />
            <Stat label="并列最优数量" value={result.tieCount} highlight />
            <Stat label="耗时" value={`${result.elapsedMs} ms`} />
          </div>

          <div className="transform-card">
            <h3>规范变换（姿态顺序 → 纵移 → 横移 裁决）</h3>
            <p>
              姿态 <strong>{result.transform.poseLabel}</strong>
              <code>{result.transform.pose}</code>，纵移 dy ={' '}
              <strong>{result.transform.dy}</strong>，横移 dx ={' '}
              <strong>{result.transform.dx}</strong>
            </p>
            <p className="hint">
              对复检图施加该变换后与参考图重合 {result.maxOverlap} 点；
              {result.tieCount > 1
                ? `另有 ${result.tieCount - 1} 个 (姿态, dy, dx) 并列最优，此处展示规范解。`
                : '该最优解唯一。'}
              {result.overlay.recheckOutOfCanvas > 0 &&
                ` ${result.overlay.recheckOutOfCanvas} 个复检缺陷经变换后移出画布（仍计入复检图总数）。`}
            </p>
          </div>

          <div className="overlay-card">
            <h3>红蓝叠加证据</h3>
            <OverlayCanvas n={result.n} overlay={result.overlay} />
            <div className="legend">
              <span><i className="sw sw-blue" />参考图缺陷（{result.overlay.matched.length + result.overlay.referenceOnly.length}）</span>
              <span><i className="sw sw-red" />复检图变换后（{result.overlay.matched.length + result.overlay.recheckOnly.length}）</span>
              <span><i className="sw sw-purple" />重合（{result.overlay.matched.length}）</span>
              <span><i className="sw sw-gray" />画布外扩展区域</span>
            </div>
          </div>

          <details className="raw">
            <summary>原始响应 JSON</summary>
            <pre>{JSON.stringify(result, null, 2)}</pre>
          </details>
        </section>
      )}

      <footer className="footer">
        输入为 16–512 的 0/1 方阵文本；非法尺寸、字符、行宽会定位到具体输入与行列。
      </footer>
    </div>
  )
}

function MatrixInput({ id, label, value, onChange, invalid }) {
  return (
    <div className={`matrix-input ${invalid ? 'invalid' : ''}`}>
      <div className="matrix-input-head">
        <label htmlFor={id}>{label}</label>
        <span className="meta">{lineCount(value)} 行 / {value.length} 字符</span>
      </div>
      <textarea
        id={id}
        spellCheck={false}
        placeholder={'粘贴 0/1 方阵文本，每行等宽，行数等于行宽（16–512）'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  )
}

function Stat({ label, value, hint, highlight }) {
  return (
    <div className={`stat ${highlight ? 'stat-highlight' : ''}`}>
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
      {hint && <div className="stat-hint">{hint}</div>}
    </div>
  )
}
