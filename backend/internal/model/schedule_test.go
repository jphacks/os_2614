package model

import (
	"bytes"
	"math"
	"reflect"
	"testing"
)

func TestEncodeEmbeddingLittleEndian(t *testing.T) {
	got := EncodeEmbedding([]float32{1})
	want := []byte{0x00, 0x00, 0x80, 0x3f}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeEmbedding([1]) = % x, want % x", got, want)
	}
}

func TestEmbeddingRoundTrip(t *testing.T) {
	cases := map[string][]float32{
		"empty":     {},
		"single":    {1.5},
		"negatives": {-1, 0, 0.25, -0.25},
		"extreme":   {math.MaxFloat32, math.SmallestNonzeroFloat32},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := DecodeEmbedding(EncodeEmbedding(in))
			if len(in) == 0 {
				if got != nil {
					t.Fatalf("Decode(Encode([])) = %v, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, in) {
				t.Fatalf("round trip = %v, want %v", got, in)
			}
		})
	}
}

func TestDecodeEmbeddingInvalid(t *testing.T) {
	if got := DecodeEmbedding(nil); got != nil {
		t.Fatalf("Decode(nil) = %v, want nil", got)
	}
	if got := DecodeEmbedding([]byte{1, 2, 3}); got != nil {
		t.Fatalf("Decode(3 bytes) = %v, want nil", got)
	}
}
