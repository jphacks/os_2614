package service

import "strings"

// geohashBase32 は標準の geohash 英数字（小文字、a・i・l・o を含まない）。
const geohashBase32 = "0123456789bcdefghjkmnpqrstuvwxyz"

const (
	geohashMinLat = -90.0
	geohashMaxLat = 90.0
	geohashMinLon = -180.0
	geohashMaxLon = 180.0
)

// clampLat は緯度を [-90, 90] に、clampLon は経度を [-180, 180] にクランプする。
func clampLat(lat float64) float64 {
	if lat < geohashMinLat {
		return geohashMinLat
	}
	if lat > geohashMaxLat {
		return geohashMaxLat
	}
	return lat
}

func clampLon(lon float64) float64 {
	if lon < geohashMinLon {
		return geohashMinLon
	}
	if lon > geohashMaxLon {
		return geohashMaxLon
	}
	return lon
}

// EncodeGeohash は lat/lon を geohash の文字列（length 桁）に符号化する。
// length <= 0 なら空文字列を返す。lat は [-90,90]、lon は [-180,180] にクランプする。
func EncodeGeohash(lat, lon float64, length int) string {
	if length <= 0 {
		return ""
	}
	lat = clampLat(lat)
	lon = clampLon(lon)

	latLo, latHi := geohashMinLat, geohashMaxLat
	lonLo, lonHi := geohashMinLon, geohashMaxLon

	var sb strings.Builder
	sb.Grow(length)

	bit := 0
	ch := 0
	even := true // 偶数ビットは経度、奇数ビットは緯度（経度から開始）。
	for sb.Len() < length {
		if even {
			mid := (lonLo + lonHi) / 2
			if lon >= mid {
				ch = ch<<1 | 1
				lonLo = mid
			} else {
				ch <<= 1
				lonHi = mid
			}
		} else {
			mid := (latLo + latHi) / 2
			if lat >= mid {
				ch = ch<<1 | 1
				latLo = mid
			} else {
				ch <<= 1
				latHi = mid
			}
		}
		even = !even
		bit++
		if bit == 5 {
			sb.WriteByte(geohashBase32[ch])
			bit = 0
			ch = 0
		}
	}
	return sb.String()
}

// DecodeGeohash は geohash を復号し、セルの中心の緯度経度を返す。
// 空・不正な文字を含む場合は ok=false。大文字は小文字として受け付ける。
func DecodeGeohash(hash string) (lat, lon float64, ok bool) {
	if hash == "" {
		return 0, 0, false
	}

	latLo, latHi := geohashMinLat, geohashMaxLat
	lonLo, lonHi := geohashMinLon, geohashMaxLon

	even := true
	for i := 0; i < len(hash); i++ {
		c := hash[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		idx := strings.IndexByte(geohashBase32, c)
		if idx < 0 {
			return 0, 0, false
		}
		for b := 4; b >= 0; b-- {
			bit := (idx >> uint(b)) & 1
			if even {
				mid := (lonLo + lonHi) / 2
				if bit == 1 {
					lonLo = mid
				} else {
					lonHi = mid
				}
			} else {
				mid := (latLo + latHi) / 2
				if bit == 1 {
					latLo = mid
				} else {
					latHi = mid
				}
			}
			even = !even
		}
	}
	return (latLo + latHi) / 2, (lonLo + lonHi) / 2, true
}

// GeohashCellCenter は lat/lon を含む geohash（length 桁）のセル中心を返す。
// length <= 0 の場合は入力座標をクランプしたものを返す。
func GeohashCellCenter(lat, lon float64, length int) (centerLat, centerLon float64) {
	if length <= 0 {
		return clampLat(lat), clampLon(lon)
	}
	centerLat, centerLon, ok := DecodeGeohash(EncodeGeohash(lat, lon, length))
	if !ok {
		return clampLat(lat), clampLon(lon)
	}
	return centerLat, centerLon
}
