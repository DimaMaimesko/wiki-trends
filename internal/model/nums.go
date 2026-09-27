package model

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// Nums is a float slice whose JSON form uses `null` for missing observations.
//
// encoding/json refuses NaN, but NaN is the only in-memory representation that
// keeps "no data" from silently behaving like zero in arithmetic. Nums bridges
// the two: NaN <-> null, round-tripping exactly.
type Nums []float64

func (n Nums) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, v := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			b.WriteString("null")
			continue
		}
		if v == math.Trunc(v) && math.Abs(v) < 1e15 {
			b.WriteString(strconv.FormatInt(int64(v), 10))
		} else {
			b.WriteString(strconv.FormatFloat(v, 'f', 4, 64))
		}
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

func (n *Nums) UnmarshalJSON(data []byte) error {
	var raw []*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(Nums, len(raw))
	for i, p := range raw {
		if p == nil {
			out[i] = math.NaN()
		} else {
			out[i] = *p
		}
	}
	*n = out
	return nil
}

// Floats exposes the slice for numeric code that expects plain []float64.
func (n Nums) Floats() []float64 { return []float64(n) }
