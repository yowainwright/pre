package diagnostics

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

const maxStringBytes = 240

func addAttr(event Event, key string, value any) {
	if !safeKey(key) || value == nil {
		return
	}
	if safe, ok := safeValue(value); ok {
		event[key] = safe
	}
}

func safeKey(key string) bool {
	if key == "" || len(key) > 80 {
		return false
	}
	for _, r := range key {
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func safeValue(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		return sanitizeString(v, maxStringBytes), true
	case bool, int, int64, uint64, float64:
		return v, true
	case int32:
		return int64(v), true
	case uint:
		return uint64(v), true
	case time.Duration:
		return v.Milliseconds(), true
	default:
		return nil, false
	}
}

func sanitizeString(value string, limit int) string {
	value = strings.Map(safeRune, strings.TrimSpace(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func safeRune(r rune) rune {
	if r == '\n' || r == '\r' || r == '\t' {
		return -1
	}
	if r < 0x20 || r == 0x7f {
		return -1
	}
	return r
}

func typeName(value any) string {
	t := reflect.TypeOf(value)
	if t == nil {
		return ""
	}
	if t.PkgPath() == "" {
		return t.String()
	}
	return fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
}
