//go:build unit

package service

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlugifyIdeaTag(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase", "GoLang", "golang"},
		{"trim", "  你好  ", "你好"},
		{"cjk keep", "联系方式", "联系方式"},
		{"spaces to dash", "hello world", "hello-world"},
		{"collapse dashes", "a  b", "a-b"},
		{"strip punct", "c++ / c#", "c-c"},
		{"empty", "   ", ""},
		{"mixed", "AI 绘画/生图", "ai-绘画-生图"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, SlugifyIdeaTag(c.in))
		})
	}
}

func TestHitsIdeaTagBlacklist(t *testing.T) {
	cases := []struct {
		name      string
		tags      []string
		blacklist []string
		want      bool
	}{
		{"empty blacklist", []string{"ad"}, nil, false},
		{"no match", []string{"tech"}, []string{"ad", "contact"}, false},
		{"exact match", []string{"ad"}, []string{"ad"}, true},
		{"match among many", []string{"go", "ad", "ai"}, []string{"ad"}, true},
		{"trims tag", []string{" ad "}, []string{"ad"}, true},
		{"ignores empty blacklist entry", []string{"ad"}, []string{"", " "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, hitsIdeaTagBlacklist(c.tags, c.blacklist))
		})
	}
}

func TestIdeaAssetMimeHelpers(t *testing.T) {
	require.Equal(t, "image/jpeg", normalizeIdeaAssetMime("", "a.JPG"))
	require.Equal(t, "image/png", normalizeIdeaAssetMime("application/octet-stream", "a.png"))
	require.Equal(t, "image/png", normalizeIdeaAssetMime("image/png", "a.png"))
	require.Equal(t, "application/pdf", normalizeIdeaAssetMime("", "doc.pdf"))

	require.True(t, isAllowedIdeaAssetMime("image/png"))
	require.True(t, isAllowedIdeaAssetMime("application/pdf"))
	require.False(t, isAllowedIdeaAssetMime("text/html"))

	require.True(t, isDangerousIdeaAssetMime("text/html"))
	require.True(t, isDangerousIdeaAssetMime("image/svg+xml"))
	require.False(t, isDangerousIdeaAssetMime("image/png"))
}

func TestSafeExt(t *testing.T) {
	require.Equal(t, ".png", safeExt("anything", "image/png"))
	require.Equal(t, ".jpg", safeExt("anything", "image/jpeg"))
	require.Equal(t, ".bin", safeExt("noext", "application/octet-stream"))
	require.Equal(t, ".md", safeExt("readme.md", "text/markdown"))
}

func encodeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestDecodeIdeaAssetImage(t *testing.T) {
	t.Run("valid png returns dimensions", func(t *testing.T) {
		w, h, err := decodeIdeaAssetImage(encodeTestPNG(t, 320, 240), "image/png")
		require.NoError(t, err)
		require.Equal(t, 320, w)
		require.Equal(t, 240, h)
	})

	t.Run("mismatched declared type rejected", func(t *testing.T) {
		_, _, err := decodeIdeaAssetImage(encodeTestPNG(t, 10, 10), "image/jpeg")
		require.ErrorIs(t, err, ErrIdeaAssetTypeInvalid)
	})

	t.Run("non-image bytes rejected", func(t *testing.T) {
		_, _, err := decodeIdeaAssetImage([]byte("this is not an image"), "image/png")
		require.ErrorIs(t, err, ErrIdeaAssetTypeInvalid)
	})

	t.Run("oversized dimension rejected", func(t *testing.T) {
		_, _, err := decodeIdeaAssetImage(encodeTestPNG(t, ideaAssetMaxImageDimension+1, 1), "image/png")
		require.ErrorIs(t, err, ErrIdeaAssetTooLarge)
	})
}
