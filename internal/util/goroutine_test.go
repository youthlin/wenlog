package util

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestGo_NormalExecution(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	Go(context.Background(), func() {
		wg.Done()
	})
	// 等待 goroutine 完成，最多等 1 秒
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("Go() did not execute the function within 1 second")
	}
}

func TestGo_PanicRecovery(t *testing.T) {
	// Go() 应该捕获 panic 而不让进程崩溃
	var wg sync.WaitGroup
	wg.Add(1)
	Go(context.Background(), func() {
		defer wg.Done()
		panic("test panic in goroutine")
	})
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// panic 被捕获，goroutine 正常结束
	case <-time.After(time.Second):
		t.Fatal("Go() did not recover from panic within 1 second")
	}
}

func TestGo_MultipleGoroutines(t *testing.T) {
	var wg sync.WaitGroup
	n := 10
	wg.Add(n)
	for i := 0; i < n; i++ {
		Go(context.Background(), func() {
			wg.Done()
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		// all goroutines completed
	case <-time.After(time.Second):
		t.Fatal("Go() did not execute all goroutines within 1 second")
	}
}
