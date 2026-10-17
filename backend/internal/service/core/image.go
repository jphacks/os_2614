package core

import (
	"bytes"
	"image"
	"image/jpeg"
	"math"

	"golang.org/x/image/draw"

	"github.com/applemango/chain/internal/httpx"
)

// media.go（instance のメディア）と profile_image.go（プロフィール画像）が共有する画像処理（10 §4.4、13 §1.3）。
// 再エンコードで EXIF（位置情報を含む）を落とす。

const (
	// MediaMaxBytes は受け付けるファイルの上限（10 §4.4）。超えたら 413。
	MediaMaxBytes    = 15 << 20
	mediaJPEGQuality = 85
)

// InvalidMedia は画像の不正（フィールド file）を 400 にする。
func InvalidMedia(reason string) *httpx.Error {
	return httpx.ErrInvalidRequest.With("fields", map[string]string{"file": reason})
}

// MaxInt は大きい方を返す。
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ResizeLongSide は長辺を maxSide に収める（既に収まっていればそのまま）。
func ResizeLongSide(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	longest := w
	if h > longest {
		longest = h
	}
	if longest <= maxSide || longest == 0 {
		return img
	}
	scale := float64(maxSide) / float64(longest)
	nw := MaxInt(1, int(math.Round(float64(w)*scale)))
	nh := MaxInt(1, int(math.Round(float64(h)*scale)))
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// EncodeJPEG は JPEG に再エンコードする。
func EncodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: mediaJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FitWithin は縦横比を保ったまま maxW×maxH に収まるよう縮小する。既に収まっていればそのまま返す。
func FitWithin(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	scale := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return ScaleToSize(img, MaxInt(1, int(math.Round(float64(w)*scale))), MaxInt(1, int(math.Round(float64(h)*scale))))
}

// ScaleToFactor は縦横比を保ったまま factor 倍に縮小する。
func ScaleToFactor(img image.Image, factor float64) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	return ScaleToSize(img, MaxInt(1, int(math.Round(float64(w)*factor))), MaxInt(1, int(math.Round(float64(h)*factor))))
}

// ScaleToSize は w×h に縮小する。
func ScaleToSize(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// EncodeJPEGQuality は指定の品質で JPEG に再エンコードする。
func EncodeJPEGQuality(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
