package service

import (
	"context"
	"reflect"
	"time"
)

// This conservative original-source work bound precedes all detached cloning.
// It counts excluded fields too; a source may fit the codec but fail preparation.
// Such failure never changes live publication. The independent codec bound still
// validates the complete frozen document and its encoded bytes after cloning.
type runtimeConfigurationCaptureBudgetV1 struct {
	ctx     context.Context
	bytes   int
	entries int
}

func runtimeConfigurationCaptureSourceBudgetV1(ctx context.Context, data *runtimeData) bool {
	if ctx == nil || ctx.Err() != nil || data == nil {
		return false
	}
	budget := runtimeConfigurationCaptureBudgetV1{ctx: ctx}
	return budget.value(reflect.ValueOf(data), 0)
}

func (b *runtimeConfigurationCaptureBudgetV1) add(bytes, entries int) bool {
	if b.ctx.Err() != nil || bytes < 0 || entries < 0 || bytes > runtimeConfigurationMaxBytesV1-b.bytes || entries > runtimeConfigurationMaxEntriesV1-b.entries {
		return false
	}
	b.bytes += bytes
	b.entries += entries
	return true
}

func (b *runtimeConfigurationCaptureBudgetV1) value(v reflect.Value, depth int) bool {
	if depth > 64 || !v.IsValid() || !b.add(16, 1) {
		return false
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return b.add(64, 0)
	}
	switch v.Kind() {
	case reflect.Pointer:
		return v.IsNil() || b.value(v.Elem(), depth+1)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !b.value(v.Field(i), depth+1) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Len() > runtimeConfigurationMaxEntriesV1-b.entries {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !b.value(v.Index(i), depth+1) {
				return false
			}
		}
	case reflect.Map:
		if v.Len() > runtimeConfigurationMaxEntriesV1-b.entries {
			return false
		}
		iterator := v.MapRange()
		for iterator.Next() {
			if !b.value(iterator.Key(), depth+1) || !b.value(iterator.Value(), depth+1) {
				return false
			}
		}
	case reflect.String:
		return b.add(v.Len(), 0)
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
	return true
}
