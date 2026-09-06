package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/image/webp"
)

func TestStudioThumbnailWebP(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		width, height, wantW, wantH int
		alpha                       uint8
	}{
		{"landscape", 1536, 768, 768, 384, 255},
		{"portrait", 600, 1200, 384, 768, 255},
		{"transparent small image", 64, 32, 64, 32, 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height))
			for y := 0; y < tc.height; y++ {
				for x := 0; x < tc.width; x++ {
					src.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), 120, tc.alpha})
				}
			}
			var raw bytes.Buffer
			require.NoError(t, png.Encode(&raw, src))
			original := bytes.Clone(raw.Bytes())
			data, mime, err := makeStudioThumbnail(context.Background(), raw.Bytes())
			require.NoError(t, err)
			require.Equal(t, "image/webp", mime)
			decoded, err := webp.Decode(bytes.NewReader(data))
			require.NoError(t, err)
			require.Equal(t, image.Rect(0, 0, tc.wantW, tc.wantH), decoded.Bounds())
			_, _, _, alpha := decoded.At(tc.wantW/2, tc.wantH/2).RGBA()
			require.InDelta(t, int(tc.alpha)*257, int(alpha), 257)
			require.Equal(t, original, raw.Bytes(), "original bytes must remain unchanged")
		})
	}
}

func TestStudioThumbnailRejectsInvalidOrOversizedImage(t *testing.T) {
	_, _, err := makeStudioThumbnail(context.Background(), []byte("not an image"))
	require.Error(t, err)
	var raw bytes.Buffer
	require.NoError(t, png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	data := raw.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 32768)
	binary.BigEndian.PutUint32(data[20:24], 32768)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	_, _, err = makeStudioThumbnail(context.Background(), data)
	require.ErrorContains(t, err, "dimensions exceed")
}
