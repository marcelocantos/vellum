// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

// TestEndpointSchemaMatchesConvert is the ENT-011 ratchet. A field added
// to convert.Endpoint or convert.FilePair fails here until the MCP DTO
// and the hand-written copier both carry it.
func TestEndpointSchemaMatchesConvert(t *testing.T) {
	checkStructCopy(t, reflect.TypeOf(convert.Endpoint{}), reflect.TypeOf(Endpoint{}), func(src reflect.Value) reflect.Value {
		ep := src.Interface().(Endpoint)
		return reflect.ValueOf(toEndpoint(&ep))
	})
	checkStructCopy(t, reflect.TypeOf(convert.FilePair{}), reflect.TypeOf(FilePair{}), func(src reflect.Value) reflect.Value {
		return reflect.ValueOf(toFilePair(src.Interface().(FilePair)))
	})
	if got := toEndpoint(nil); !reflect.DeepEqual(got, convert.Endpoint{}) {
		t.Fatalf("nil endpoint mapped to %#v", got)
	}
}

func checkStructCopy(t *testing.T, dst, src reflect.Type, copyFn func(reflect.Value) reflect.Value) {
	t.Helper()
	dstFields := exportedFields(dst)
	srcFields := exportedFields(src)
	if len(dstFields) != len(srcFields) {
		t.Fatalf("%s has %d exported fields, %s has %d", dst, len(dstFields), src, len(srcFields))
	}
	for _, df := range dstFields {
		sf, ok := src.FieldByName(df.Name)
		if !ok || !sf.IsExported() {
			t.Fatalf("%s missing exported field %s", src, df.Name)
		}
		if !fieldTypeCompatible(df.Type, sf.Type) {
			t.Fatalf("%s.%s type %s is not compatible with %s", src, df.Name, sf.Type, df.Type)
		}
	}
	filled := reflect.New(src).Elem()
	for _, sf := range srcFields {
		if err := fillValue(filled.FieldByName(sf.Name), sf.Name); err != nil {
			t.Fatal(err)
		}
	}
	got := copyFn(filled)
	if got.Type() != dst {
		t.Fatalf("copier returned %s, want %s", got.Type(), dst)
	}
	for _, df := range dstFields {
		have := got.FieldByName(df.Name)
		want := filled.FieldByName(df.Name)
		if !valuesEqual(have, want) {
			t.Errorf("%s.%s not copied: got %#v want %#v", dst.Name(), df.Name, have.Interface(), want.Interface())
		}
	}
}

func exportedFields(typ reflect.Type) []reflect.StructField {
	var out []reflect.StructField
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.IsExported() {
			out = append(out, f)
		}
	}
	return out
}

func fieldTypeCompatible(dst, src reflect.Type) bool {
	if dst == src {
		return true
	}
	if dst.Kind() == reflect.String && src.Kind() == reflect.String {
		return true
	}
	if dst.Kind() == reflect.Slice && src.Kind() == reflect.Slice {
		return fieldTypeCompatible(dst.Elem(), src.Elem())
	}
	return false
}

func fillValue(v reflect.Value, token string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString("v-" + token)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Slice:
		elem := reflect.New(v.Type().Elem()).Elem()
		if err := fillValue(elem, token); err != nil {
			return err
		}
		v.Set(reflect.Append(v, elem))
	default:
		return fmt.Errorf("unsupported field kind %s", v.Kind())
	}
	return nil
}

func valuesEqual(got, want reflect.Value) bool {
	if got.Kind() == reflect.String && want.Kind() == reflect.String {
		return got.String() == want.String()
	}
	if got.Kind() == reflect.Slice && want.Kind() == reflect.Slice {
		if got.Len() != want.Len() {
			return false
		}
		for i := range got.Len() {
			if !valuesEqual(got.Index(i), want.Index(i)) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got.Interface(), want.Interface())
}
