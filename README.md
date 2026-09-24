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
        （可观察验收：27 项 [PASS]/[FAIL]）
```

- `backend/`：Gin API。`POST /api/audit` 提交两幅 0/1 方阵文本，`GET /api/health` 健康检查；
  `POST /api/counter-evidence` 发起最小遮挡反证复核（边长 16–96）。
- `frontend/`：React（Vite 构建）+ nginx，展示规范变换、最大重合数、并列最优数量、红蓝叠加证据，
  并在叠图旁发起反证复核、把可行窗口叠绘在叠图上。
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

### `POST /api/counter-evidence`

最小遮挡反证复核。复检员怀疑规范姿态只因局部遮挡偶然胜出时，在页面叠图旁输入一个**非规范候选**
（非规范姿态、纵移、横移），服务端**重新解析两幅原图并重跑既有精确配准**（与 `/api/audit` 同一套
FFT 相关搜索 + 整数复核），绝不信任浏览器保存的最大重合数或变换。该入口仅支持边长 **16–96**，
并按「姿态 → 纵移 → 横移」的既有顺序校验目标候选（姿态 0..7、平移 ∈ [-(N-1), N-1]，
且不得与规范候选完全相同）。

请求：

```json
{ "reference": "0101...\n...", "recheck": "...",
  "targetPose": 0, "targetDy": 0, "targetDx": 0 }
```

对每个参考缺陷编码命中差：规范命中记 +1、目标命中记 −1（取值 -1/0/1）。移除某轴对齐非空矩形 W
内的参考缺陷后目标严格胜过规范 ⟺ 窗内权值和 ≥ gap+1（gap = 规范重合 − 目标重合）。
后端在**所有**轴对齐非空矩形中精确找面积最小者；同面积按 (上, 左, 下, 右) 坐标裁决；
**自行处理负权**（枚举上下边界 + 列和前缀精确扫描，不是依赖前缀单调的贪心滑窗）。
N=96 最坏情形实测约 8 ms。

响应（200，可反证）：

```json
{
  "n": 16, "feasible": true,
  "canonical": { "poseIndex": 0, "pose": "identity", "poseLabel": "…", "dy": 1, "dx": 0, "overlap": 6 },
  "target":    { "poseIndex": 0, "pose": "identity", "poseLabel": "…", "dy": 0, "dx": 0, "overlap": 5 },
  "gap": 1,
  "window": {
    "top": 10, "left": 4, "bottom": 10, "right": 5,
    "width": 2, "height": 1, "area": 2,
    "removedCanonical": 2, "removedTarget": 0,
    "removedPoints": [[10, 4], [10, 5]],
    "canonicalOverlapAfter": 4, "targetOverlapAfter": 5
  }
}
```

不可反证时 `feasible=false` 且 `window` 为 JSON `null`——不伪造矩形。非法候选（姿态/纵移/横移
越界、缺字段、与规范候选相同、N>96、原图解析失败）返回 400 并定位字段（原图错误仍定位到行列）；
计算异常返回 500。`/api/audit` 的请求/响应与页面主审计流程保持不变。

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
5. 后端直连恒等用例，排除代理因素；
6. 最小遮挡反证复核：真实页面已挂载复核入口与打包调用、从页面同源入口提交一个可反证案例
   （服务端重算规范候选、独立暴力参照核对 -1/0/1 权值最小窗口与移除前后重合）、
   一个不可反证案例（无窗口且不伪造矩形），以及姿态/纵移/横移越界、N=97 拒绝、
   候选与规范相同、原图非法字符重新解析定位等 400 用例；原有审计用例全部仍须通过。
