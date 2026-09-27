package stats

import "math"

// LM is a small multiple-regression helper used where a second regressor is
// needed alongside the time index (currently: structural-break detection).
type LM struct {
	Beta []float64
	SE   []float64
	SSR  float64
	N    int
	Lag  int
}

// invert returns the inverse of a small symmetric positive-definite matrix by
// Gauss-Jordan elimination with partial pivoting. k is 2-4 here, so neither
// speed nor a cleverer factorisation matters.
func invert(a [][]float64) [][]float64 {
	k := len(a)
	m := make([][]float64, k)
	for i := range m {
		m[i] = make([]float64, 2*k)
		copy(m[i], a[i])
		m[i][k+i] = 1
	}
	for c := 0; c < k; c++ {
		p := c
		for r := c + 1; r < k; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		if math.Abs(m[p][c]) < 1e-12 {
			return nil
		}
		m[c], m[p] = m[p], m[c]
		pv := m[c][c]
		for j := 0; j < 2*k; j++ {
			m[c][j] /= pv
		}
		for r := 0; r < k; r++ {
			if r == c || m[r][c] == 0 {
				continue
			}
			f := m[r][c]
			for j := 0; j < 2*k; j++ {
				m[r][j] -= f * m[c][j]
			}
		}
	}
	out := make([][]float64, k)
	for i := range out {
		out[i] = m[i][k:]
	}
	return out
}

// FitOLS runs y = X*beta with Newey-West standard errors when lag > 0. Rows of X
// that pair with a NaN y are dropped. X must already include an intercept column
// if one is wanted.
func FitOLS(X [][]float64, y []float64, lag int) *LM {
	var xs [][]float64
	var ys []float64
	for i, v := range y {
		if math.IsNaN(v) {
			continue
		}
		bad := false
		for _, c := range X[i] {
			if math.IsNaN(c) {
				bad = true
				break
			}
		}
		if bad {
			continue
		}
		xs = append(xs, X[i])
		ys = append(ys, v)
	}
	n, k := len(ys), len(X[0])
	if n <= k+2 {
		return nil
	}
	xtx := make([][]float64, k)
	for i := range xtx {
		xtx[i] = make([]float64, k)
	}
	xty := make([]float64, k)
	for r := 0; r < n; r++ {
		for i := 0; i < k; i++ {
			xty[i] += xs[r][i] * ys[r]
			for j := i; j < k; j++ {
				xtx[i][j] += xs[r][i] * xs[r][j]
			}
		}
	}
	for i := 0; i < k; i++ {
		for j := 0; j < i; j++ {
			xtx[i][j] = xtx[j][i]
		}
	}
	inv := invert(xtx)
	if inv == nil {
		return nil
	}
	beta := make([]float64, k)
	for i := 0; i < k; i++ {
		for j := 0; j < k; j++ {
			beta[i] += inv[i][j] * xty[j]
		}
	}
	u := make([]float64, n)
	ssr := 0.0
	for r := 0; r < n; r++ {
		f := 0.0
		for i := 0; i < k; i++ {
			f += xs[r][i] * beta[i]
		}
		u[r] = ys[r] - f
		ssr += u[r] * u[r]
	}
	out := &LM{Beta: beta, SSR: ssr, N: n, Lag: lag}

	// sandwich variance: inv * Omega * inv
	omega := make([][]float64, k)
	for i := range omega {
		omega[i] = make([]float64, k)
	}
	if lag <= 0 {
		s2 := ssr / float64(n-k)
		for i := 0; i < k; i++ {
			for j := 0; j < k; j++ {
				omega[i][j] = xtx[i][j] * s2
			}
		}
	} else {
		for r := 0; r < n; r++ {
			for i := 0; i < k; i++ {
				for j := 0; j < k; j++ {
					omega[i][j] += xs[r][i] * xs[r][j] * u[r] * u[r]
				}
			}
		}
		for l := 1; l <= lag && l < n; l++ {
			w := 1 - float64(l)/float64(lag+1)
			for r := l; r < n; r++ {
				for i := 0; i < k; i++ {
					for j := 0; j < k; j++ {
						omega[i][j] += w * (xs[r][i]*xs[r-l][j] + xs[r-l][i]*xs[r][j]) * u[r] * u[r-l]
					}
				}
			}
		}
	}
	out.SE = make([]float64, k)
	for i := 0; i < k; i++ {
		v := 0.0
		for a := 0; a < k; a++ {
			for b := 0; b < k; b++ {
				v += inv[i][a] * omega[a][b] * inv[b][i]
			}
		}
		if v > 0 {
			out.SE[i] = math.Sqrt(v)
		} else {
			out.SE[i] = math.NaN()
		}
	}
	return out
}

// NWLag is the Newey-West bandwidth rule floor(4*(n/100)^(2/9)).
func NWLag(n int) int {
	l := int(math.Floor(4 * math.Pow(float64(n)/100, 2.0/9.0)))
	if l < 1 {
		l = 1
	}
	if l > n/4 {
		l = n / 4
	}
	return l
}
