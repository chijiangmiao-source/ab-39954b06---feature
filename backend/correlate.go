package main

import "math"

// 正方形的八种旋转/镜像姿态（二面体群 D4），顺序固定。
// 规范解裁决时按此顺序优先，姿态内再按纵移 dy、横移 dx 升序。
var poses = []struct {
	Name  string
	Label string
}{
	{"identity", "恒等（不旋转不镜像）"},
	{"rot90", "顺时针旋转 90°"},
	{"rot180", "旋转 180°"},
	{"rot270", "顺时针旋转 270°"},
	{"flipLR", "左右镜像"},
	{"flipTB", "上下镜像"},
	{"transpose", "主对角线翻转"},
	{"antiTranspose", "副对角线翻转"},
}

// applyPose 把 N×N 方阵中的坐标 (r,c) 按第 p 种姿态映射到新坐标。
func applyPose(p, r, c, n int) (int, int) {
	switch p {
	case 0:
		return r, c
	case 1:
		return c, n - 1 - r
	case 2:
		return n - 1 - r, n - 1 - c
	case 3:
		return n - 1 - c, r
	case 4:
		return r, n - 1 - c
	case 5:
		return n - 1 - r, c
	case 6:
		return c, r
	default: // 7
		return n - 1 - c, n - 1 - r
	}
}

// fftRoots 预计算单位根，避免蝶形运算内重复复乘带来的漂移与开销。
// 这里的一维/二维 FFT 均为本项目自实现，不调用任何现成配准或二维相关接口。
type fftRoots struct {
	n     int
	roots []complex128 // roots[k] = e^(2πi·k/n)，0 <= k < n/2
}

func newFFTRoots(n int) *fftRoots {
	f := &fftRoots{n: n, roots: make([]complex128, n/2)}
	for k := 0; k < n/2; k++ {
		ang := 2 * math.Pi * float64(k) / float64(n)
		f.roots[k] = complex(math.Cos(ang), math.Sin(ang))
	}
	return f
}

// fft1 迭代基-2 一维 FFT，invert 时为逆变换并除以 n。
func (f *fftRoots) fft1(a []complex128, invert bool) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		step := f.n / length
		half := length >> 1
		for i := 0; i < n; i += length {
			k := 0
			for j := i; j < i+half; j++ {
				w := f.roots[k]
				if invert {
					w = complex(real(w), -imag(w))
				}
				v := a[j+half] * w
				a[j+half] = a[j] - v
				a[j] += v
				k += step
			}
		}
	}
	if invert {
		inv := complex(1/float64(n), 0)
		for i := range a {
			a[i] *= inv
		}
	}
}

// fft2 在 l×l 的平坦数组上做二维 FFT（先行后列）。
func (f *fftRoots) fft2(m []complex128, l int, invert bool) {
	for r := 0; r < l; r++ {
		f.fft1(m[r*l:(r+1)*l], invert)
	}
	buf := make([]complex128, l)
	for c := 0; c < l; c++ {
		for r := 0; r < l; r++ {
			buf[r] = m[r*l+c]
		}
		f.fft1(buf, invert)
		for r := 0; r < l; r++ {
			m[r*l+c] = buf[r]
		}
	}
}

// searchResult 是穷举 8 种姿态 × 全部整数平移后的规范解与统计。
type searchResult struct {
	maxOverlap int // 最大重合数
	tieCount   int // 达到最大重合的 (姿态, dy, dx) 总数
	poseIndex  int // 规范解姿态下标
	dy, dx     int // 规范解纵移、横移
}

// searchMaxOverlap 用自实现的二维 FFT 做精确整数互相关：
// 对每种姿态，一次二维相关即得到横纵各 -(N-1)..(N-1) 全部 (2N-1)^2 个整数平移的重合数，
// 因此稠密满尺寸输入的复杂度为 O(N^2 log N)，不会退化为逐点尝试全部平移。
//
// 精确性：矩阵元素为 0/1，相关值上界为 N^2 <= 262144，float64 尾数 53 位，
// L<=1024 的 FFT 舍入误差上界约 1e-10 量级，远低于 0.5，四舍五入后即为精确整数；
// 规范解还会被 countOverlapDirect 用纯整数逐一复核。
func searchMaxOverlap(ref, rec [][]uint8, n int) searchResult {
	l := 1
	for l < 2*n {
		l <<= 1
	}
	roots := newFFTRoots(l)

	fa := make([]complex128, l*l)
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if ref[r][c] != 0 {
				fa[r*l+c] = 1
			}
		}
	}
	roots.fft2(fa, l, false)

	fb := make([]complex128, l*l)
	res := searchResult{maxOverlap: -1}
	for p := 0; p < 8; p++ {
		clear(fb)
		for r := 0; r < n; r++ {
			for c := 0; c < n; c++ {
				if rec[r][c] != 0 {
					pr, pc := applyPose(p, r, c, n)
					fb[pr*l+pc] = 1
				}
			}
		}
		roots.fft2(fb, l, false)
		for i := range fb {
			b := fb[i]
			fb[i] = fa[i] * complex(real(b), -imag(b)) // fa * conj(fb) => 互相关
		}
		roots.fft2(fb, l, true)
		// 循环相关的下标 k 对应平移 k（k < n）或 k-l（k >= l-(n-1)），l >= 2n-1 保证不混叠。
		for dy := -(n - 1); dy <= n-1; dy++ {
			ky := dy
			if ky < 0 {
				ky += l
			}
			row := fb[ky*l : ky*l+l]
			for dx := -(n - 1); dx <= n-1; dx++ {
				kx := dx
				if kx < 0 {
					kx += l
				}
				v := int(math.Round(real(row[kx])))
				// 严格大于才替换：姿态顺序、dy、dx 均升序遍历，首个最大值即规范解。
				if v > res.maxOverlap {
					res.maxOverlap = v
					res.tieCount = 1
					res.poseIndex = p
					res.dy, res.dx = dy, dx
				} else if v == res.maxOverlap {
					res.tieCount++
				}
			}
		}
	}
	return res
}

// countOverlapDirect 纯整数直接计数：复检图经姿态 p 与平移 (dy,dx) 后落在参考图缺陷上的点数。
// 移出画布的缺陷不计入重合（但仍属于原图缺陷总数，由调用方统计）。
func countOverlapDirect(ref, rec [][]uint8, n, p, dy, dx int) int {
	cnt := 0
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if rec[r][c] == 0 {
				continue
			}
			pr, pc := applyPose(p, r, c, n)
			tr, tc := pr+dy, pc+dx
			if tr >= 0 && tr < n && tc >= 0 && tc < n && ref[tr][tc] != 0 {
				cnt++
			}
		}
	}
	return cnt
}
