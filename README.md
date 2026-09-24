# 晶圆缺陷复检审计台

晶圆复检设备会因装片方向与视野偏移，把同一批缺陷记录成**旋转、镜像且平移**后的点阵。
本审计台对参考图与复检图做**穷举式精确匹配**：遍历正方形的 8 种旋转/镜像姿态，
以及横纵各 `-(N-1) .. (N-1)` 的整数平移，用自实现的精确整数二维相关求出最大重合数，
并按固定裁决顺序给出规范解，避免抽样叠图或浮点配准选错对称姿态。

## 架构

```
┌──────────┐   /api 同源代理   ┌──────────┐
│ frontend │ ───────────────▶ │ backend  │
│ React +  │                  │ Go (Gin) │
│ nginx    │ ◀─────────────── │ 精确相关 │
└──────────┘                  └──────────┘
     ▲                              ▲
     └────────── verify ────────────┘
        （可观察验收：16 项 [PASS]/[FAIL]）
```

- `backend/`：Gin API。`POST /api/audit` 提交两幅 0/1 方阵文本，`GET /api/health` 健康检查。
- `frontend/`：React（Vite 构建）+ nginx，展示规范变换、最大重合数、并列最优数量与红蓝叠加证据。
- `verify/`：Compose 中名为 `verify` 的验收服务，等待前后端健康后执行端到端用例并打印结果。

## 核心算法（backend/correlate.go）

- **姿态群**：正方形二面体群 D4 共 8 种（恒等、旋转 90°/180°/270°、左右/上下镜像、主/副对角线翻转），顺序固定。
- **平移范围**：横纵各 `-(N-1) .. (N-1)`，共 `(2N-1)^2` 个整数平移；移出画布的缺陷不计入重合，但仍计入原图缺陷总数。
- **精确整数二维相关**：对每种姿态，用**自实现的二维 FFT**（迭代基-2 + 预计算单位根，未调用任何现成配准/相关库）
  一次算出全部 `(2N-1)^2` 个平移的重合数，复杂度 `O(N² log N)`——
  稠密满尺寸（512×512 全 1）也不会退化为逐点尝试全部平移，实测单次审计约 0.7 s。
- **精确性**：矩阵元素为 0/1，相关值 ≤ N² ≤ 262144，float64（53 位尾数）在 L≤1024 的 FFT 下
  舍入误差约 1e-10 量级，远低于 0.5，四舍五入即为精确整数；规范解另由 `countOverlapDirect` 纯整数复核，
  不一致则整个请求返回 500，不留下任何结论。
- **规范解裁决**：按姿态顺序（0..7）→ 纵移 dy 升序 → 横移 dx 升序，首个达到最大重合者即规范解；
  同时统计并列最优（达到最大重合的 `(姿态, dy, dx)` 总数）。
- 正确性由 `backend/correlate_test.go` 保证：多尺寸多密度随机输入下，FFT 搜索结果与暴力穷举完全一致。

## API

### `POST /api/audit`

请求：

```json
{ "reference": "0101...\n...", "recheck": "..." }
```

两幅图均为边长 16–512 的 0/1 方阵文本（行数 = 行宽，两图边长须一致）。

响应（200）：

```json
{
  "n": 16,
  "referenceCount": 12,
  "recheckCount": 12,
  "maxOverlap": 12,
  "tieCount": 1,
  "transform": { "poseIndex": 1, "pose": "rot90", "poseLabel": "顺时针旋转 90°", "dy": 2, "dx": -1 },
  "overlay": {
    "matched": [[4, 4], "..."],
    "referenceOnly": [],
    "recheckOnly": [],
    "recheckOutOfCanvas": 0
  },
  "elapsedMs": 3
}
```

`overlay` 坐标系与参考图画布一致；`recheckOnly` 可能包含画布外坐标（前端扩展视野绘制）。

输入非法（400），定位到具体输入、行、列：

```json
{ "error": { "field": "recheck", "line": 3, "column": 5, "message": "非法字符 'x'，仅允许 0 和 1" } }
```

计算异常（500）不携带任何结论字段；前端在任何失败时都会清空旧结论，修正输入后可原样重试。

## 运行（Docker Compose）

```bash
docker compose up --build -d        # 启动 frontend + backend
docker compose run --rm verify      # 执行可观察验收（或 compose up 时自动跑一次）
docker compose logs verify          # 查看 16 项 [PASS]/[FAIL] 与汇总
```

- 前端默认 <http://localhost:8080>，后端默认 <http://localhost:8081>。
- 宿主机端口可配置：复制 `.env.example` 为 `.env`，修改 `FRONTEND_PORT` / `BACKEND_PORT`，
  或直接 `FRONTEND_PORT=9000 docker compose up -d`。
- 健康检查：backend 轮询 `/api/health`，frontend 轮询 `/`；`verify` 依赖两者健康后执行。

## 本地开发

```bash
# 后端（Go 1.23+）
cd backend && go test ./... && go run .          # 监听 :8080（PORT 可覆盖）

# 前端（Node 20+）
cd frontend && npm ci && npm run dev             # :5173，/api 代理到 VITE_API_TARGET（默认 :8081）
npm run build && npm run preview                 # 生产构建预览 :4173，同样代理 /api
```

## 验收覆盖（verify/main.go）

1. 后端 `/api/health` 就绪、前端首页与 `#root` 挂载点可达；
2. 经前端 nginx 代理的端到端用例：确定性姿态+平移、镜像姿态、移出画布计数、
   64×64 与 512×512 稠密满尺寸（断言最大重合、并列数、规范解与耗时）；
3. 小尺寸用例均与 verify 内置暴力穷举参照逐字段比对，并校验叠加证据与规范变换自洽；
4. 非法字符（定位到行/列）、行宽不一致、非方阵、边长越界、两图边长不一致、空输入等 400 用例；
5. 后端直连恒等用例，排除代理因素。
