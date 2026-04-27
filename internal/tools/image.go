package tools

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/charmbracelet/x/mosaic"
)

func (r *Registry) execPreviewImage(argsJSON string) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	args.Path = strings.TrimSpace(args.Path)
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if args.Width <= 0 {
		args.Width = 80
	}
	if args.Width > 160 {
		args.Width = 160
	}
	if args.Height > 80 {
		args.Height = 80
	}

	path := r.resolvePath(args.Path)
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open image %s: %w", args.Path, err)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode image %s: %w", args.Path, err)
	}

	bounds := img.Bounds()
	renderer := mosaic.New().Width(args.Width)
	if args.Height > 0 {
		renderer = renderer.Height(args.Height)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "image preview %s (%s, %dx%d)\n", args.Path, format, bounds.Dx(), bounds.Dy())
	sb.WriteString(renderer.Render(img))
	return strings.TrimRight(sb.String(), "\n"), nil
}
