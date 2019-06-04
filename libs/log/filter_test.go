package log

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/go-kit/kit/log"
)

func TestFilter_With(t *testing.T) {
	logger := NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = NewFilter(logger, AllowError(), AllowInfoWith("module", "test1"), AllowInfoWith("module", "test2"))

	logger1 := logger.With("module", "test3")
	f, ok := logger1.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError, f.allowed)
}

func TestFilter_UpdateWith(t *testing.T) {
	logger := NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = NewFilter(logger, AllowError(), AllowInfoWith("module", "test1"), AllowInfoWith("module", "test2"))
	cacheLoggers = &CacheLoggers{
		loggersMap: make(map[string]Logger),
		allowedKV:  cacheLoggers.allowedKV,
	}

	logger3 := logger.With("module", "test3")
	f, ok := logger3.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError, f.allowed)

	defaultOption := AllowInfo()
	options := []Option{AllowDebugWith("module", "test1")}
	UpdateFilter(defaultOption, options...)

	logger1 := logger.With("module", "test1")
	f, ok = logger1.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError|levelInfo|levelDebug, f.allowed)

	logger2 := logger.With("module", "test2")
	f, ok = logger2.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError|levelInfo, f.allowed)

	f, ok = logger3.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError|levelInfo, f.allowed)

	defaultOption = AllowInfo()
	options = []Option{AllowDebugWith("module", "test3")}
	UpdateFilter(defaultOption, options...)
	f, ok = logger3.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError|levelInfo|levelDebug, f.allowed)
	require.Equal(t, 3, len(f.allowedKV.data))
}

func TestConcurrent(t *testing.T) {
	logger := NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = NewFilter(logger, AllowError(), AllowInfoWith("module", "test1"), AllowInfoWith("module", "test2"))
	cacheLoggers = &CacheLoggers{
		loggersMap: make(map[string]Logger),
		allowedKV:  cacheLoggers.allowedKV,
	}

	chan1 := make(chan struct{})
	chan2 := make(chan struct{})

	round := 2000

	go func() {
		for i := 0; i < round; i++ {
			//fmt.Printf("chan 1: %d\n", i)
			tmp := logger.With("module", fmt.Sprintf("test%d", i))
			tmp.Error("kv")
			tmp.Info("kv")
			tmp.Debug("kv")
		}
		chan1 <- struct{}{}
	}()

	go func() {
		for i := 0; i < round; i++ {
			//fmt.Printf("chan 2: %d\n", i)
			UpdateLogLevel(fmt.Sprintf("test%d:info,main:info,state:info,order:info,distribution:debug,auth:info,token:info,*:error", i))
			UpdateLogLevel(fmt.Sprintf("test%d:debug,*.debug", i))
			UpdateLogLevel(fmt.Sprintf("test%d:erro,*info", i))
		}
		chan2 <- struct{}{}
	}()

	<-chan1
	<-chan2
}

func TestFilter_UpdateWith2(t *testing.T) {
	logger := NewTMLogger(log.NewSyncWriter(os.Stdout))
	logger = NewFilter(logger, AllowError())
	cacheLoggers = &CacheLoggers{
		loggersMap: make(map[string]Logger),
		allowedKV:  cacheLoggers.allowedKV,
	}

	logger3 := logger.With("module", "test3")
	f, ok := logger3.(*filter)
	require.True(t, ok)
	require.Equal(t, levelError, f.allowed)

	UpdateLogLevel(fmt.Sprintf("test3:debug"))
	require.Equal(t, levelError|levelInfo|levelDebug, f.allowed)

}
