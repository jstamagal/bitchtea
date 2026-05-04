package tools

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewImage_validPNG(t *testing.T) {
	dir := t.TempDir()
	writePreviewTestPNG(t, filepath.Join(dir, "tiny.png"), 2, 2)

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "preview_image", `{"path":"tiny.png","width":4}`)
	if err != nil {
		t.Fatalf("preview_image: %v", err)
	}
	if !strings.Contains(result, "image preview tiny.png (png, 2x2)") {
		t.Fatalf("unexpected preview header: %q", result)
	}
}

func TestPreviewImage_missingFile(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir, t.TempDir())

	_, err := reg.Execute(context.Background(), "preview_image", `{"path":"missing.png"}`)
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if !strings.Contains(err.Error(), "open image missing.png") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreviewImage_unsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-image.txt"), []byte("plain text"), 0644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}
	reg := NewRegistry(dir, t.TempDir())

	_, err := reg.Execute(context.Background(), "preview_image", `{"path":"not-image.txt"}`)
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
	if !strings.Contains(err.Error(), "decode image not-image.txt") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreviewImage_largeImageClampsRequestedSize(t *testing.T) {
	dir := t.TempDir()
	writePreviewTestPNG(t, filepath.Join(dir, "large.png"), 320, 240)

	reg := NewRegistry(dir, t.TempDir())
	result, err := reg.Execute(context.Background(), "preview_image", `{"path":"large.png","width":999,"height":999}`)
	if err != nil {
		t.Fatalf("preview_image: %v", err)
	}
	if !strings.Contains(result, "image preview large.png (png, 320x240)") {
		t.Fatalf("unexpected preview header: %q", result)
	}
	lines := strings.Split(result, "\n")
	if len(lines) > 81 {
		t.Fatalf("expected header plus at most 80 rendered rows, got %d lines", len(lines))
	}
}

func writePreviewTestPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("encode image: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image: %v", err)
	}
}
