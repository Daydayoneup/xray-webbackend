package service

import (
	"fmt"
	"strings"
)

// DiffLine 是一行 diff：Op 为 ' '（相同）、'-'（删除）、'+'（新增）。
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// LineDiff 计算两段文本的逐行差异（LCS），只保留改动附近 context 行，其余折叠。
func LineDiff(a, b string, context int) []DiffLine {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	n, m := len(x), len(y)
	// 跳过公共前后缀，降低 LCS 规模
	pre := 0
	for pre < n && pre < m && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && x[n-1-suf] == y[m-1-suf] {
		suf++
	}
	xs, ys := x[pre:n-suf], y[pre:m-suf]
	dp := make([][]int, len(xs)+1)
	for i := range dp {
		dp[i] = make([]int, len(ys)+1)
	}
	for i := len(xs) - 1; i >= 0; i-- {
		for j := len(ys) - 1; j >= 0; j-- {
			if xs[i] == ys[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var all []DiffLine
	for _, l := range x[:pre] {
		all = append(all, DiffLine{" ", l})
	}
	i, j := 0, 0
	for i < len(xs) || j < len(ys) {
		switch {
		case i < len(xs) && j < len(ys) && xs[i] == ys[j]:
			all = append(all, DiffLine{" ", xs[i]})
			i, j = i+1, j+1
		case i < len(xs) && (j == len(ys) || dp[i+1][j] >= dp[i][j+1]):
			all = append(all, DiffLine{"-", xs[i]})
			i++
		default:
			all = append(all, DiffLine{"+", ys[j]})
			j++
		}
	}
	for _, l := range x[n-suf:] {
		all = append(all, DiffLine{" ", l})
	}
	// 折叠远离改动的相同行
	keep := make([]bool, len(all))
	for k, d := range all {
		if d.Op != " " {
			for t := k - context; t <= k+context; t++ {
				if t >= 0 && t < len(all) {
					keep[t] = true
				}
			}
		}
	}
	var out []DiffLine
	skipped := 0
	for k, d := range all {
		if keep[k] {
			if skipped > 0 {
				out = append(out, DiffLine{"@", fmt.Sprintf("… 省略 %d 行相同内容 …", skipped)})
				skipped = 0
			}
			out = append(out, d)
		} else {
			skipped++
		}
	}
	if skipped > 0 && len(out) > 0 {
		out = append(out, DiffLine{"@", fmt.Sprintf("… 省略 %d 行相同内容 …", skipped)})
	}
	return out
}
