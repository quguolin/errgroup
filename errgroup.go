package errgroup

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

type ErrorGroup struct {
	err         error
	wg          sync.WaitGroup
	recoverOnce sync.Once
	initOnce    sync.Once
	chFunc      chan func(ctx context.Context) error
	chFuncs     []func(ctx context.Context) error
	ctx         context.Context
	cancel      func()
}

func WithContext(ctx context.Context) *ErrorGroup {
	return &ErrorGroup{ctx: ctx}
}

func WithCancel(ctx context.Context) *ErrorGroup {
	ctx, cancel := context.WithCancel(ctx)
	return &ErrorGroup{ctx: ctx, cancel: cancel}
}

func (eg *ErrorGroup) do(f func(ctx context.Context) error) {
	ctx := eg.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	defer func() {
		if r := recover(); r != nil {
			buffer := make([]byte, 64<<10)
			buffer = buffer[:runtime.Stack(buffer, false)]
			err = fmt.Errorf("errgroup: panic recovered: %s\n%s", r, buffer)
		}
		if err != nil {
			eg.recoverOnce.Do(func() {
				eg.err = err
				if eg.cancel != nil {
					eg.cancel()
				}
			})
		}
		eg.wg.Done()
	}()
	err = f(ctx)
}

func (eg *ErrorGroup) GOMAXPROCS(count int) {
	if count <= 0 {
		panic("errgroup: GOMAXPROCS must great than 0")
	}
	eg.initOnce.Do(func() {
		eg.chFunc = make(chan func(context.Context) error, count)
		for index := 0; index < count; index++ {
			go func() {
				for f := range eg.chFunc {
					eg.do(f)
				}
			}()
		}
	})
}

func (eg *ErrorGroup) Go(f func(ctx context.Context) error) {
	eg.wg.Add(1)
	if eg.chFunc != nil {
		select {
		case eg.chFunc <- f:
		default:
			eg.chFuncs = append(eg.chFuncs, f)
		}
		return
	}
	go eg.do(f)
}

func (eg *ErrorGroup) Wait() error {
	if eg.chFunc != nil {
		for _, f := range eg.chFuncs {
			eg.chFunc <- f
		}
	}
	eg.wg.Wait()
	if eg.chFunc != nil {
		close(eg.chFunc) // let all receiver exit
	}
	if eg.cancel != nil {
		eg.cancel()
	}
	return eg.err
}
