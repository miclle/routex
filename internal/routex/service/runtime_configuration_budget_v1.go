package service

import (
	"context"
	"reflect"
	"strconv"
	"time"
	"unicode/utf8"
)

// The closed frozen root is counted before JSON allocation, so encoder input
// cannot use huge strings or collections to bypass the decoder's document bound.
type runtimeConfigurationBudgetV1 struct {
	ctx     context.Context
	bytes   int
	entries int
}

func (b *runtimeConfigurationBudgetV1) add(bytes, entries int) bool {
	if b.ctx.Err() != nil || bytes < 0 || entries < 0 || bytes > runtimeConfigurationMaxBytesV1-b.bytes || entries > runtimeConfigurationMaxEntriesV1-b.entries {
		return false
	}
	b.bytes += bytes
	b.entries += entries
	return true
}
func (b *runtimeConfigurationBudgetV1) text(text string, entry bool) bool {
	n := 0
	if entry {
		n = 1
	}
	if !b.add(2, n) || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		size := utf8.RuneLen(r)
		switch r {
		case '\\', '"', '\b', '\f', '\n', '\r', '\t':
			size = 2
		case '<', '>', '&', '\u2028', '\u2029':
			size = 6
		default:
			if r < 32 {
				size = 6
			}
		}
		if !b.add(size, 0) {
			return false
		}
	}
	return true
}
func (b *runtimeConfigurationBudgetV1) value(v reflect.Value) bool {
	if b.ctx.Err() != nil {
		return false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return b.add(4, 1)
		}
		return b.value(v.Elem())
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return b.text(v.Interface().(time.Time).Format(time.RFC3339Nano), true)
	}
	switch v.Kind() {
	case reflect.Struct:
		if !b.add(2+max(0, v.NumField()-1)+v.NumField(), 2) {
			return false
		}
		for i := 0; i < v.NumField(); i++ {
			if !b.text(v.Type().Field(i).Name, true) || !b.value(v.Field(i)) {
				return false
			}
		}
	case reflect.Map:
		if v.IsNil() {
			return b.add(4, 1)
		}
		if !b.add(2+max(0, v.Len()-1)+v.Len(), 2) {
			return false
		}
		iterator := v.MapRange()
		for iterator.Next() {
			if !b.text(iterator.Key().String(), true) || !b.value(iterator.Value()) {
				return false
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			return b.add(4, 1)
		}
		if !b.add(2+max(0, v.Len()-1), 2) {
			return false
		}
		for i := 0; i < v.Len(); i++ {
			if !b.value(v.Index(i)) {
				return false
			}
		}
	case reflect.String:
		return b.text(v.String(), true)
	case reflect.Bool:
		if v.Bool() {
			return b.add(4, 1)
		}
		return b.add(5, 1)
	case reflect.Int, reflect.Int64:
		return b.add(len(strconv.FormatInt(v.Int(), 10)), 1)
	default:
		return false
	}
	return true
}
func configurationEncodeBudgetV1(ctx context.Context, document runtimeConfigurationV1) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	// The encoder fills this fixed-size sibling digest after structural validation.
	document.PricingObservationDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	budget := runtimeConfigurationBudgetV1{ctx: ctx}
	return budget.value(reflect.ValueOf(document))
}
