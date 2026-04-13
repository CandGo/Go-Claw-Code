package native

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

const maxVisionDimension = 1568

// ResizeImage resizes an image to fit within maxW x maxH while maintaining aspect ratio.
func ResizeImage(data []byte, maxW, maxH int) ([]byte, error) {
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxW && h <= maxH {
		return data, nil // no resize needed
	}

	resized := imaging.Fit(img, maxW, maxH, imaging.Lanczos)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, resized, imaging.PNG); err != nil {
		return nil, fmt.Errorf("failed to encode resized image: %w", err)
	}
	return buf.Bytes(), nil
}

// ResizeForVision resizes an image to fit within model vision constraints (1568x1568).
func ResizeForVision(data []byte) ([]byte, error) {
	return ResizeImage(data, maxVisionDimension, maxVisionDimension)
}

// EncodeToBase64 encodes raw image bytes to a base64 data URI string.
func EncodeToBase64(data []byte, mediaType string) string {
	return fmt.Sprintf("data:%s;base64,%s", mediaType, base64.StdEncoding.EncodeToString(data))
}

// DetectMediaType detects image media type from file extension.
func DetectMediaType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	default:
		return "image/png"
	}
}

// ImageDimensions returns the width and height of an image.
func ImageDimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

// EncodePNG encodes an image.Image to PNG bytes.
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CompressForVision compresses an image to JPEG for sending to vision models.
// quality: JPEG quality 1-100 (75 recommended)
// maxDim: max dimension for resizing (1024 recommended)
func CompressForVision(data []byte, quality int, maxDim int) ([]byte, error) {
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Resize if either dimension exceeds maxDim
	if w > maxDim || h > maxDim {
		img = imaging.Fit(img, maxDim, maxDim, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(quality)); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// ResizeForVisionJPEG compresses an image to JPEG with sensible defaults
// for vision models: quality=75, max dimension=1024.
// Typically reduces 1920x1080 PNG (~2MB) to ~100-200KB JPEG.
func ResizeForVisionJPEG(data []byte) ([]byte, error) {
	return CompressForVision(data, 75, 1024)
}
