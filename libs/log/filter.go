package log

import (
	"fmt"
	"strings"
	"sync"
)

type level byte

const (
	levelDebug level = 1 << iota
	levelInfo
	levelError
	keyvalsSplit = "&"
)

type allowedKeyvalMap struct {
	sync.RWMutex
	data map[keyval]level // When key-value match, use this level
}

func (a *allowedKeyvalMap) set(key interface{}, value interface{}, lv level) {
	a.Lock()
	defer a.Unlock()
	a.data[keyval{key, value}] = lv
}

func (a *allowedKeyvalMap) traverse(f func(keyval, level) (bool, *filter)) (bool, *filter) {
	a.RLock()
	defer a.RUnlock()
	for kv, allowed := range a.data {
		re, f := f(kv, allowed)
		if re {
			return re, f
		}
	}
	return false, nil
}

type filter struct {
	next             Logger
	allowed          level             // XOR'd levels for default case
	initiallyAllowed level             // XOR'd levels for initial case
	allowedKV        *allowedKeyvalMap // When key-value match, use this level
}

type keyval struct {
	key   interface{}
	value interface{}
}

type CacheLoggers struct {
	sync.RWMutex
	allowedKV  *allowedKeyvalMap
	loggersMap map[string]Logger
}

var once sync.Once
var cacheLoggers *CacheLoggers

func (cl *CacheLoggers) update(defaultOption Option, options ...Option) {

	cl.Lock()
	defer cl.Unlock()

	for _, option := range options {
		option(&filter{allowedKV: cl.allowedKV})
	}

	for k, v := range cl.loggersMap {
		l, ok := v.(*filter)
		if !ok {
			continue
		}

		if defaultOption != nil {
			defaultOption(l)
		}

		l.initiallyAllowed = l.allowed // allowed: default * allowed
		ks := strings.Split(k, keyvalsSplit)
		l.UpdateWith(ks...)
	}
}

func UpdateFilter(defaultOption Option, options ...Option) {
	loggers := getLoggers()
	loggers.update(defaultOption, options...)
}

func (l *CacheLoggers) get(key string) Logger {
	l.RLock()
	defer l.RUnlock()
	if value, ok := l.loggersMap[key]; ok {
		return value
	}
	return nil
}

func (l *CacheLoggers) set(key string, logger Logger) {
	l.Lock()
	defer l.Unlock()
	l.loggersMap[key] = logger
}

func getLoggers() *CacheLoggers {
	once.Do(func() {
		cacheLoggers = &CacheLoggers{
			loggersMap: make(map[string]Logger),
		}
	})
	return cacheLoggers
}

// NewFilter wraps next and implements filtering. See the commentary on the
// Option functions for a detailed description of how to configure levels. If
// no options are provided, all leveled log events created with Debug, Info or
// Error helper methods are squelched.
func NewFilter(next Logger, options ...Option) Logger {

	allowedKV := &allowedKeyvalMap{data: make(map[keyval]level)}
	kv := keyval{"module", ""}
	allowedKV.data[kv] = levelError

	loggerMap := getLoggers()
	loggerMap.allowedKV = allowedKV


	l := &filter{
		next:      next,
		allowedKV: loggerMap.allowedKV,
	}

	for _, option := range options {
		option(l)
	}

	l.initiallyAllowed = l.allowed
	return l
}

func (l *filter) Info(msg string, keyvals ...interface{}) {
	levelAllowed := l.allowed&levelInfo != 0
	if !levelAllowed {
		return
	}
	l.next.Info(msg, keyvals...)
}

func (l *filter) Debug(msg string, keyvals ...interface{}) {
	levelAllowed := l.allowed&levelDebug != 0
	if !levelAllowed {
		return
	}
	l.next.Debug(msg, keyvals...)
}

func (l *filter) Error(msg string, keyvals ...interface{}) {
	levelAllowed := l.allowed&levelError != 0
	if !levelAllowed {
		return
	}
	l.next.Error(msg, keyvals...)
}

// With implements Logger by constructing a new filter with a keyvals appended
// to the logger.
//
// If custom level was set for a keyval pair using one of the
// Allow*With methods, it is used as the logger's level.
//
// Examples:
//     logger = log.NewFilter(logger, log.AllowError(), log.AllowInfoWith("module", "crypto"))
//		 logger.With("module", "crypto").Info("Hello") # produces "I... Hello module=crypto"
//
//     logger = log.NewFilter(logger, log.AllowError(), log.AllowInfoWith("module", "crypto"), log.AllowNoneWith("user", "Sam"))
//		 logger.With("module", "crypto", "user", "Sam").Info("Hello") # returns nil
//
//     logger = log.NewFilter(logger, log.AllowError(), log.AllowInfoWith("module", "crypto"), log.AllowNoneWith("user", "Sam"))
//		 logger.With("user", "Sam").With("module", "crypto").Info("Hello") # produces "I... Hello module=crypto user=Sam"
func (l *filter) With(keyvals ...interface{}) Logger {
	keyInallowedKeyvalMap := false
	var keyvalsStr string
	for _, kv := range keyvals {
		s, ok := kv.(string)
		if !ok {
			return &filter{
				next:             l.next.With(keyvals...),
				allowed:          l.allowed, // simply continue with the current level
				allowedKV:        l.allowedKV,
				initiallyAllowed: l.initiallyAllowed,
			}
		}
		keyvalsStr += s
		keyvalsStr += keyvalsSplit
	}
	keyvalsStr = strings.Trim(keyvalsStr, keyvalsSplit)
	loggers := getLoggers()
	log := loggers.get(keyvalsStr)
	if log != nil {
		return log
	}

	for i := len(keyvals) - 2; i >= 0; i -= 2 {
		traverseFunc := func(kv keyval, allowed level) (bool, *filter) {
			if keyvals[i] == kv.key {
				keyInallowedKeyvalMap = true
				// Example:
				//		logger = log.NewFilter(logger, log.AllowError(), log.AllowInfoWith("module", "crypto"))
				//		logger.With("module", "crypto")
				if keyvals[i+1] == kv.value {
					f := &filter{
						next:             l.next.With(keyvals...),
						allowed:          allowed, // set the desired level
						allowedKV:        l.allowedKV,
						initiallyAllowed: l.initiallyAllowed,
					}
					return true, f
				}
			}
			return false, nil
		}

		re, f := l.allowedKV.traverse(traverseFunc)
		if re {
			loggers.set(keyvalsStr, f)
			return f
		}
	}

	// Example:
	//		logger = log.NewFilter(logger, log.AllowError(), log.AllowInfoWith("module", "crypto"))
	//		logger.With("module", "main")
	if keyInallowedKeyvalMap {
		f := &filter{
			next:             l.next.With(keyvals...),
			allowed:          l.initiallyAllowed, // return back to initially allowed
			allowedKV:        l.allowedKV,
			initiallyAllowed: l.initiallyAllowed,
		}
		loggers.set(keyvalsStr, f)
		return f
	}

	f := &filter{
		next:             l.next.With(keyvals...),
		allowed:          l.allowed, // simply continue with the current level
		allowedKV:        l.allowedKV,
		initiallyAllowed: l.initiallyAllowed,
	}
	return f
}


func (l *filter) UpdateWith(keyvals ...string) {
	keyInallowedKeyvalMap := false

	for i := len(keyvals) - 2; i >= 0; i -= 2 {

		traverseFunc := func(kv keyval, allowed level) (bool, *filter) {
			if keyvals[i] != kv.key {
				return false, nil
			}

			keyInallowedKeyvalMap = true

			if keyvals[i+1] != kv.value {
				return false, nil
			}
			l.allowed = allowed // set the desired level
			return true, nil
		}

		re, _ := l.allowedKV.traverse(traverseFunc)
		if re {
			return
		}
	}

	if keyInallowedKeyvalMap {
		l.allowed = l.initiallyAllowed // return back to initially allowed
	}
}

//--------------------------------------------------------------------------------

// Option sets a parameter for the filter.
type Option func(*filter)

// AllowLevel returns an option for the given level or error if no option exist
// for such level.
func AllowLevel(lvl string) (Option, error) {
	switch lvl {
	case "debug":
		return AllowDebug(), nil
	case "info":
		return AllowInfo(), nil
	case "error":
		return AllowError(), nil
	case "none":
		return AllowNone(), nil
	default:
		return nil, fmt.Errorf("Expected either \"info\", \"debug\", \"error\" or \"none\" level, given %s", lvl)
	}
}

// AllowAll is an alias for AllowDebug.
func AllowAll() Option {
	return AllowDebug()
}

// AllowDebug allows error, info and debug level log events to pass.
func AllowDebug() Option {
	return allowed(levelError | levelInfo | levelDebug)
}

// AllowInfo allows error and info level log events to pass.
func AllowInfo() Option {
	return allowed(levelError | levelInfo)
}

// AllowError allows only error level log events to pass.
func AllowError() Option {
	return allowed(levelError)
}

// AllowNone allows no leveled log events to pass.
func AllowNone() Option {
	return allowed(0)
}

func allowed(allowed level) Option {
	return func(l *filter) {
		l.allowed = allowed
	}
}

// AllowDebugWith allows error, info and debug level log events to pass for a specific key value pair.
func AllowDebugWith(key interface{}, value interface{}) Option {
	return func(l *filter) {
		l.allowedKV.set(key, value, levelError|levelInfo|levelDebug)
	}
}

// AllowInfoWith allows error and info level log events to pass for a specific key value pair.
func AllowInfoWith(key interface{}, value interface{}) Option {
	return func(l *filter) {
		l.allowedKV.set(key, value, levelError|levelInfo)
	}
}

// AllowErrorWith allows only error level log events to pass for a specific key value pair.
func AllowErrorWith(key interface{}, value interface{}) Option {
	return func(l *filter) {
		l.allowedKV.set(key, value, levelError)
	}
}

// AllowNoneWith allows no leveled log events to pass for a specific key value pair.
func AllowNoneWith(key interface{}, value interface{}) Option {
	return func(l *filter) {
		l.allowedKV.set(key, value, 0)
	}
}

func UpdateLogLevel(lvl string) error {
	if lvl == "" {
		return fmt.Errorf("Empty log level")
	}

	defaultLogLevelKey := "*"
	l := lvl

	// prefix simple one word levels (e.g. "info") with "*"
	if !strings.Contains(l, ":") {
		l = defaultLogLevelKey + ":" + l
	}

	options := make([]Option, 0)

	var defaultOption Option // for module *
	var err error

	list := strings.Split(l, ",")
	for _, item := range list {
		moduleAndLevel := strings.Split(item, ":")

		if len(moduleAndLevel) != 2 {
			return fmt.Errorf("Expected list in a form of \"module:level\" pairs, given pair %s, list %s", item, list)
		}

		module := moduleAndLevel[0]
		level := moduleAndLevel[1]

		var option Option
		if module == defaultLogLevelKey {
			defaultOption, err = AllowLevel(level)
			if err != nil {
				return err
			}
		} else {
			switch level {
			case "debug":
				option = AllowDebugWith("module", module)
			case "info":
				option = AllowInfoWith("module", module)
			case "error":
				option = AllowErrorWith("module", module)
			case "none":
				option = AllowNoneWith("module", module)
			default:
				return fmt.Errorf("Expected either \"info\", \"debug\", \"error\" or \"none\" log level, given %s (pair %s, list %s)",
					level, item, list)
			}
			options = append(options, option)
		}
	}

	UpdateFilter(defaultOption, options...)
	return nil
}
