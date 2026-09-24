// 与后端 Gin API 交互的真实 HTTP 调用。

export async function fetchHealth() {
  const r = await fetch('/api/health', { cache: 'no-store' })
  if (!r.ok) throw new Error(`健康检查失败：HTTP ${r.status}`)
  return r.json()
}

export class ApiError extends Error {
  constructor(status, payload) {
    const e = payload && payload.error ? payload.error : {}
    super(e.message || `请求失败：HTTP ${status}`)
    this.status = status
    this.field = e.field || ''
    this.line = e.line || 0
    this.column = e.column || 0
  }
}

export async function postAudit(reference, recheck) {
  let r
  try {
    r = await fetch('/api/audit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reference, recheck }),
    })
  } catch (err) {
    throw new ApiError(0, { error: { message: `无法连接后端：${err.message}` } })
  }
  let data = null
  try {
    data = await r.json()
  } catch {
    // 非 JSON 响应
  }
  if (!r.ok) {
    throw new ApiError(r.status, data)
  }
  return data
}
