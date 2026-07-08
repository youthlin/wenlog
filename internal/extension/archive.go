package extension

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ExtractOptions 定义扩展包 zip 解压限制。
type ExtractOptions struct {
	Kind       string
	MaxSize    int64
	MaxFile    int64
	MaxFiles   int
	MaxNameLen int
	AllowFile  func(name string) bool
}

// ExtractZip 解压 zip 到目标目录，包含路径穿越和资源上限防护。
func ExtractZip(r io.ReaderAt, size int64, dest string, opts ExtractOptions) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	kind := opts.Kind
	if kind == "" {
		kind = "extension"
	}
	var total int64
	var files int
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if !SafeRelativePath(name) {
			continue
		}
		if opts.MaxNameLen > 0 && len(filepath.Base(name)) > opts.MaxNameLen {
			return fmt.Errorf("%s file name too long: %s", kind, name)
		}
		target := filepath.Join(dest, name)
		if !PathWithinDir(dest, target) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		files++
		if opts.MaxFiles > 0 && files > opts.MaxFiles {
			return fmt.Errorf("%s package contains too many files", kind)
		}
		headerSize := int64(f.UncompressedSize64)
		if opts.MaxFile > 0 && headerSize > opts.MaxFile {
			return fmt.Errorf("%s file %s is too large", kind, name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if opts.AllowFile != nil && !opts.AllowFile(name) {
			continue
		}
		written, err := extractZipFile(f, target, opts.MaxFile, kind)
		if err != nil {
			return err
		}
		total += written
		if opts.MaxSize > 0 && total > opts.MaxSize {
			_ = os.Remove(target)
			return fmt.Errorf("%s package uncompressed size is too large", kind)
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string, maxFile int64, kind string) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	written, copyErr := copyLimited(out, rc, maxFile, kind)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return written, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return written, closeErr
	}
	return written, nil
}

func copyLimited(out io.Writer, rc io.Reader, maxFile int64, kind string) (int64, error) {
	if maxFile <= 0 {
		return io.Copy(out, rc)
	}
	lr := &io.LimitedReader{R: rc, N: maxFile + 1}
	written, err := io.Copy(out, lr)
	if err != nil {
		return written, err
	}
	if written > maxFile {
		return written, fmt.Errorf("%s file is too large", kind)
	}
	return written, nil
}

// WriteZip 把目录打包成以 rootName 为根目录名的 zip。
func WriteZip(w io.Writer, rootName, dir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(rootName, rel))
		if d.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
