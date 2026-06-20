// Package imageutil 提供图片缩略图生成能力。
package imageutil

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/disintegration/imaging"
)

// ThumbSize 预定义的缩略图尺寸（宽x高，0 表示等比缩放不限高）。
type ThumbSize struct {
	Width  int
	Height int // 0 表示等比缩放
	Crop   bool
}

// ThumbSizes WordPress 兼容的缩略图尺寸。
var ThumbSizes = []ThumbSize{
	{150, 150, true},  // 裁剪为正方形
	{300, 300, true},  // 裁剪为正方形
	{768, 0, false},   // 等比缩放，不限高
}

// ThumbInfo 单个缩略图信息。
type ThumbInfo struct {
	Width  int
	Path   string // 相对于原图目录的文件名，如 xxx-150x150.png
	URL    string // 站内 URL，如 /wp-content/uploads/2026/06/xxx-150x150.png
}

// GenerateThumbnails 为指定图片生成多尺寸缩略图（WordPress 命名兼容）。
// absPath 是原图的绝对路径，urlPath 是原图的站内 URL（如 /wp-content/uploads/2026/06/xxx.png）。
// 返回生成的缩略图信息列表。如果原图宽度小于目标宽度则跳过该尺寸。
func GenerateThumbnails(absPath, urlPath string) ([]ThumbInfo, error) {
	src, err := imaging.Open(absPath)
	if err != nil {
		return nil, errors.Wrap(err, "open source image")
	}

	bounds := src.Bounds()
	origWidth := bounds.Dx()

	dir := filepath.Dir(absPath)
	ext := filepath.Ext(absPath)
	base := strings.TrimSuffix(filepath.Base(absPath), ext)
	urlDir := filepath.Dir(urlPath)

	var thumbs []ThumbInfo
	for _, ts := range ThumbSizes {
		if origWidth <= ts.Width {
			continue
		}
		var suffix string
		if ts.Height > 0 {
			suffix = fmt.Sprintf("%dx%d", ts.Width, ts.Height)
		} else {
			suffix = fmt.Sprintf("%dw", ts.Width)
		}
		thumbName := fmt.Sprintf("%s-%s%s", base, suffix, ext)
		thumbPath := filepath.Join(dir, thumbName)
		thumbURL := filepath.ToSlash(filepath.Join(urlDir, thumbName))

		var resized *image.NRGBA
		if ts.Crop {
			resized = imaging.Fill(src, ts.Width, ts.Height, imaging.Center, imaging.Lanczos)
		} else {
			resized = imaging.Fit(src, ts.Width, ts.Width*10, imaging.Lanczos)
		}
		if err := imaging.Save(resized, thumbPath); err != nil {
			return thumbs, errors.Wrapf(err, "save thumbnail %s", thumbName)
		}
		thumbs = append(thumbs, ThumbInfo{Width: ts.Width, Path: thumbPath, URL: thumbURL})
	}

	return thumbs, nil
}

// RemoveThumbnails 删除原图对应的所有缩略图文件。
func RemoveThumbnails(absPath string) {
	dir := filepath.Dir(absPath)
	ext := filepath.Ext(absPath)
	base := strings.TrimSuffix(filepath.Base(absPath), ext)

	for _, ts := range ThumbSizes {
		var suffix string
		if ts.Height > 0 {
			suffix = fmt.Sprintf("%dx%d", ts.Width, ts.Height)
		} else {
			suffix = fmt.Sprintf("%dw", ts.Width)
		}
		thumbPath := filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, suffix, ext))
		_ = os.Remove(thumbPath)
	}
}

// 确保 image 包被引用
var _ = image.Point{}
