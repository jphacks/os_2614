package service

import "math"

// geoPoint は照合に使う緯度経度の 1 点（09 §8.4）。
type geoPoint struct {
	Lat, Lon float64
}

// dbscan は eps（m）以内を近いとみなす DBSCAN で、minPts 人以上の塊を返す（09 §8.4）。
// 人数が少ない前提の素朴な O(n²) 実装。返すのは入力の添字の列。
func dbscan(points []geoPoint, eps float64, minPts int) [][]int {
	const (
		unvisited = 0
		noise     = -1
	)
	n := len(points)
	labels := make([]int, n)
	neighbors := func(i int) []int {
		out := make([]int, 0, n)
		for j := 0; j < n; j++ {
			if j == i {
				continue
			}
			if Haversine(points[i].Lat, points[i].Lon, points[j].Lat, points[j].Lon) <= eps {
				out = append(out, j)
			}
		}
		return out
	}

	clusters := make([][]int, 0)
	for i := 0; i < n; i++ {
		if labels[i] != unvisited {
			continue
		}
		region := neighbors(i)
		if len(region)+1 < minPts {
			labels[i] = noise
			continue
		}
		clusters = append(clusters, []int{i})
		id := len(clusters)
		labels[i] = id
		seeds := append([]int{}, region...)
		for len(seeds) > 0 {
			q := seeds[0]
			seeds = seeds[1:]
			switch {
			case labels[q] == noise:
				labels[q] = id
				clusters[id-1] = append(clusters[id-1], q)
			case labels[q] != unvisited:
				continue
			default:
				labels[q] = id
				clusters[id-1] = append(clusters[id-1], q)
				if qr := neighbors(q); len(qr)+1 >= minPts {
					seeds = append(seeds, qr...)
				}
			}
		}
	}
	return clusters
}

// naturalComponents は edge(i, j) が真の組でつないだ連結成分を返す（09 §8.4）。
func naturalComponents(n int, edge func(i, j int) bool) [][]int {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if edge(i, j) {
				union(i, j)
			}
		}
	}

	groups := map[int][]int{}
	order := make([]int, 0, n)
	for i := 0; i < n; i++ {
		r := find(i)
		if _, ok := groups[r]; !ok {
			order = append(order, r)
		}
		groups[r] = append(groups[r], i)
	}
	out := make([][]int, 0, len(order))
	for _, r := range order {
		out = append(out, groups[r])
	}
	return out
}

// cosineSimilarity は 2 つのベクトルの cos 類似度を返す。長さが違う・零ベクトルなら 0。
func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// overlapRatio は 2 つの範囲 [start, end) の重なりを、短い方に対する割合で返す（09 §8.4）。
func overlapRatio(aStart, aEnd, bStart, bEnd int) float64 {
	inter := min(aEnd, bEnd) - max(aStart, bStart)
	if inter <= 0 {
		return 0
	}
	shortest := min(aEnd-aStart, bEnd-bStart)
	if shortest <= 0 {
		return 0
	}
	return float64(inter) / float64(shortest)
}

// medoidIndex は他の全員との cos 距離の和が最小の要素の添字を返す（09 §8.5）。空なら -1。
func medoidIndex(vectors [][]float32) int {
	if len(vectors) == 0 {
		return -1
	}
	best, bestSum := 0, math.MaxFloat64
	for i := range vectors {
		sum := 0.0
		for j := range vectors {
			if i != j {
				sum += 1 - cosineSimilarity(vectors[i], vectors[j])
			}
		}
		if sum < bestSum {
			bestSum, best = sum, i
		}
	}
	return best
}

// circle2 は 2 次元の円（m）。
type circle2 struct {
	X, Y, R float64
}

// smallestEnclosingCircle は 2 次元の点を囲む最小の円を返す（09 §8.5）。
// 決定的な順序のまま動く O(n³) の Welzl。点がなければ半径 0 の円。
func smallestEnclosingCircle(pts []circle2) (cx, cy, r float64) {
	var c circle2
	for i := range pts {
		if withinCircle(pts[i], c) {
			continue
		}
		c = circle2{X: pts[i].X, Y: pts[i].Y}
		for j := 0; j < i; j++ {
			if withinCircle(pts[j], c) {
				continue
			}
			c = circleFrom2(pts[i], pts[j])
			for k := 0; k < j; k++ {
				if withinCircle(pts[k], c) {
					continue
				}
				c = circleFrom3(pts[i], pts[j], pts[k])
			}
		}
	}
	return c.X, c.Y, c.R
}

func withinCircle(p circle2, c circle2) bool {
	return dist2(circle2{X: p.X, Y: p.Y}, c) <= c.R*c.R
}

func circleFrom2(a, b circle2) circle2 {
	return circle2{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2, R: math.Hypot(a.X-b.X, a.Y-b.Y) / 2}
}

func circleFrom3(a, b, c circle2) circle2 {
	d := 2 * (a.X*(b.Y-c.Y) + b.X*(c.Y-a.Y) + c.X*(a.Y-b.Y))
	if math.Abs(d) < 1e-9 {
		best := circleFrom2(b, c)
		if ac := circleFrom2(a, c); ac.R > best.R {
			best = ac
		}
		if ab := circleFrom2(a, b); ab.R > best.R {
			best = ab
		}
		return best
	}
	a2 := a.X*a.X + a.Y*a.Y
	b2 := b.X*b.X + b.Y*b.Y
	c2 := c.X*c.X + c.Y*c.Y
	ux := (a2*(b.Y-c.Y) + b2*(c.Y-a.Y) + c2*(a.Y-b.Y)) / d
	uy := (a2*(c.X-b.X) + b2*(a.X-c.X) + c2*(b.X-a.X)) / d
	return circle2{X: ux, Y: uy, R: math.Hypot(a.X-ux, a.Y-uy)}
}

func dist2(a, b circle2) float64 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

const metersPerDegLat = 111320.0

// naturalGeofence は自動グループの geofence を、メンバーの位置の最小外接円に余裕を足して求める（09 §8.5）。
func naturalGeofence(points []geoPoint) (lat, lon float64, radiusM int) {
	if len(points) == 0 {
		return 0, 0, NaturalRadiusMin
	}
	var lat0, lon0 float64
	for _, p := range points {
		lat0 += p.Lat
		lon0 += p.Lon
	}
	n := float64(len(points))
	lat0 /= n
	lon0 /= n
	cos := math.Cos(lat0 * math.Pi / 180)
	proj := make([]circle2, len(points))
	for i, p := range points {
		proj[i] = circle2{X: (p.Lon - lon0) * cos * metersPerDegLat, Y: (p.Lat - lat0) * metersPerDegLat}
	}
	cx, cy, r := smallestEnclosingCircle(proj)
	return lat0 + cy/metersPerDegLat, lon0 + cx/(cos*metersPerDegLat), clampNaturalRadius(r + naturalRadiusMargin)
}

func clampNaturalRadius(r float64) int {
	rr := int(math.Round(r))
	if rr < NaturalRadiusMin {
		return NaturalRadiusMin
	}
	if rr > NaturalRadiusMax {
		return NaturalRadiusMax
	}
	return rr
}
