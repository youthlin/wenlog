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

// ThumbSize 预定义的缩略图宽度。
var ThumbSizes = []int{150, 300, 768}

// ThumbInfo 单个缩略图信息。
type ThumbInfo struct {
	Width  int
	Path   string // 相对于原图目录的文件名，如 xxx-150w.png
	URL    string // 站内 URL，如 /wp-content/uploads/2026/06/xxx-150w.png
}

// GenerateThumbnails 为指定图片生成多尺寸缩略图。
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
	for _, w := range ThumbSizes {
		if origWidth <= w {
			continue // 原图已经够小，不生成更大的缩略图
		}
		thumbName := fmt.Sprintf("%s-%dw%s", base, w, ext)
		thumbPath := filepath.Join(dir, thumbName)
		thumbURL := filepath.ToSlash(filepath.Join(urlDir, thumbName))

		resized := imaging.Fit(src, w, w*10, imaging.Lanczos) // 限制宽度，高度自适应（上限 10x 防止异常）
		if err := imaging.Save(resized, thumbPath); err != nil {
			return thumbs, errors.Wrapf(err, "save thumbnail %s", thumbName)
		}
		thumbs = append(thumbs, ThumbInfo{Width: w, Path: thumbPath, URL: thumbURL})
	}

	return thumbs, nil
}

// RemoveThumbnails 删除原图对应的所有缩略图文件。
func RemoveThumbnails(absPath string) {
	dir := filepath.Dir(absPath)
	ext := filepath.Ext(absPath)
	base := strings.TrimSuffix(filepath.Base(absPath), ext)

	for _, w := range ThumbSizes {
		thumbPath := filepath.Join(dir, fmt.Sprintf("%s-%dw%s", base, w, ext))
		_ = os.Remove(thumbPath)
	}
}

// 确保 image 包被引用
var _ = image.Point{}
