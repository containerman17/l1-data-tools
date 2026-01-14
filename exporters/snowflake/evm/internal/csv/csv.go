// Package csv provides utilities to convert transform package row types to CSV format.
package csv

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

// Write writes a slice of structs to CSV format.
// Headers are derived from the csv struct tags.
func Write[T any](w io.Writer, rows []T) error {
	if len(rows) == 0 {
		return nil
	}

	cw := csv.NewWriter(w)

	// Extract headers from struct tags
	headers, err := getHeaders[T]()
	if err != nil {
		return err
	}

	if err := cw.Write(headers); err != nil {
		return err
	}

	// Write each row
	for _, row := range rows {
		record, err := toRecord(row)
		if err != nil {
			return err
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
}

// getHeaders extracts CSV headers from struct field tags.
func getHeaders[T any]() ([]string, error) {
	var t T
	rt := reflect.TypeOf(t)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", rt.Kind())
	}

	headers := make([]string, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		tag := field.Tag.Get("csv")
		if tag == "" {
			tag = field.Name
		}
		headers[i] = tag
	}
	return headers, nil
}

// toRecord converts a struct to a slice of strings for CSV.
func toRecord(v any) ([]string, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", rv.Kind())
	}

	rt := rv.Type()
	record := make([]string, rt.NumField())

	for i := 0; i < rt.NumField(); i++ {
		field := rv.Field(i)
		record[i] = formatValue(field)
	}

	return record, nil
}

// formatValue converts a reflect.Value to its string representation.
func formatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Bool:
		if v.Bool() {
			return "true"
		}
		return "false"
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}
