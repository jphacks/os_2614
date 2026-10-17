package service

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"math"

	"golang.org/x/image/draw"

	"github.com/applemango/chain/internal/httpx"
)

// プロフィールのアイコン・バナーの再エンコード（13 §1.3、10 §4.4 と同じ方針）。

const (
	// アイコンは 1000×1000、バナーは 3000×1000 に収める（13 §1.3）。正方形へのクロップはしない。
	avatarMaxWidth  = 1000
	avatarMaxHeight = 1000
	bannerMaxWidth  = 3000
	bannerMaxHeight = 1000

	// 1,000,000 バイト以下になるまで品質を下げる（85 から 5 ずつ、下限 50。それでも超えたら縮小）（13 §1.3）。
	profileJPEGMaxBytes    = 1_000_000
	profileJPEGQualityHigh = 85
	profileJPEGQualityLow  = 50
	profileJPEGQualityStep = 5
	profileShrinkFactor    = 0.9
)

// decodeProfileImage は multipart の画像を読んで形式を検証し、デコードする（13 §1.3）。
// 対応する形式は JPEG / PNG / WebP。それ以外と巨大なファイルはエラーにする。
func decodeProfileImage(r io.Reader, size int64) (image.Image, error) {
	if size > MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r, MediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MediaMaxBytes {
		return nil, httpx.ErrPayloadTooLarge
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, invalidMedia("画像として読めません")
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return nil, invalidMedia("JPEG / PNG / WebP のみ対応しています")
	}
	return img, nil
}

// encodeProfileJPEG はデコード済みの画像を maxW×maxH の枠に収め、1,000,000 バイト以下になる
// JPEG にする（13 §1.3）。再エンコードで EXIF（位置情報を含む）は落ちる。
// 品質を下限まで下げても超える場合は縮小を続ける。
func encodeProfileJPEG(img image.Image, maxW, maxH int) ([]byte, error) {
	img = fitWithin(img, maxW, maxH)
	var smallest []byte
	for attempt := 0; attempt < 16; attempt++ {
		for q := profileJPEGQualityHigh; q >= profileJPEGQualityLow; q -= profileJPEGQualityStep {
			b, err := encodeJPEGQuality(img, q)
			if err != nil {
				return nil, err
			}
			if len(b) <= profileJPEGMaxBytes {
				return b, nil
			}
			smallest = b
		}
		img = scaleToFactor(img, profileShrinkFactor)
	}
	return smallest, nil
}

// fitWithin は縦横比を保ったまま maxW×maxH に収まるよう縮小する。既に収まっていればそのまま返す。
func fitWithin(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	scale := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return scaleToSize(img, maxInt(1, int(math.Round(float64(w)*scale))), maxInt(1, int(math.Round(float64(h)*scale))))
}

func scaleToFactor(img image.Image, factor float64) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	return scaleToSize(img, maxInt(1, int(math.Round(float64(w)*factor))), maxInt(1, int(math.Round(float64(h)*factor))))
}

func scaleToSize(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func encodeJPEGQuality(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
