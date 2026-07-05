package handler

import (
	"net/http"
	"sync/atomic"
)

var _ http.FileSystem = (*LocalFirstFileSystem)(nil)
var _ hotSwitcher = (*LocalFirstFileSystem)(nil)

type hotSwitcher interface {
	Hot() bool
	SetHot(bool)
}

type LocalFirstFileSystem struct {
	Dir      string
	fallback http.FileSystem
	hot      atomic.Bool
}

func NewLocalFirstFileSystem(dir string, fallback http.FileSystem) *LocalFirstFileSystem {
	return &LocalFirstFileSystem{
		Dir:      dir,
		fallback: fallback,
	}
}

func (fsys *LocalFirstFileSystem) Open(name string) (http.File, error) {
	if fsys != nil && fsys.hot.Load() {
		return http.Dir(fsys.Dir).Open(name)
	}
	return fsys.fallback.Open(name)
}

func (fsys *LocalFirstFileSystem) Hot() bool {
	return fsys != nil && fsys.hot.Load()
}

func (fsys *LocalFirstFileSystem) SetHot(v bool) {
	if fsys == nil {
		return
	}
	fsys.hot.Store(v)
}
