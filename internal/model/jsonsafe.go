package model

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
)

// MarshalSafe serialises a value whose float fields may be NaN or infinite.
//
// Why this is needed at all: "not computed" is a real and common outcome in this
// domain — a Theil-Sen slope needs three points, Mann-Kendall needs eight monthly
// observations, an aligned year-over-year comparison needs two full years. NaN is
// the only in-memory value that keeps those absences from silently behaving like
// zero in later arithmetic, but encoding/json refuses to emit it.
//
// The alternatives were worse. Making every such field a *float64 or a custom
// float type pushes nil-checks and conversions into every reader of the struct,
// for a concern that only exists at the serialisation boundary. So the conversion
// happens exactly here: one reflective pass that maps every non-finite float to
// JSON null, leaving the in-memory model free to use NaN as intended.
//
// Types that implement json.Marshaler are passed through untouched, so Nums and
// time.Time keep their own encodings.
func MarshalSafe(v any, indent string) ([]byte, error) {
	if indent == "" {
		return json.Marshal(sanitize(reflect.ValueOf(v)))
	}
	return json.MarshalIndent(sanitize(reflect.ValueOf(v)), "", indent)
}

var marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

func sanitize(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	t := v.Type()

	// Respect custom encoders (Nums, time.Time, ...).
	if t.Implements(marshalerType) || (t.Kind() != reflect.Ptr && reflect.PointerTo(t).Implements(marshalerType)) {
		if v.Kind() == reflect.Ptr && v.IsNil() {
			return nil
		}
		if v.CanInterface() {
			return v.Interface()
		}
	}

	switch t.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return sanitize(v.Elem())

	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f

	case reflect.Struct:
		out := map[string]any{}
		flattenStruct(v, out)
		return out

	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return v.Interface() // []byte keeps base64 encoding
		}
		if t.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = sanitize(v.Index(i))
		}
		return out

	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		for _, k := range v.MapKeys() {
			out[keyString(k)] = sanitize(v.MapIndex(k))
		}
		return out
	}
	if v.CanInterface() {
		return v.Interface()
	}
	return nil
}

func flattenStruct(v reflect.Value, out map[string]any) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts := tag, ""
		if i := strings.IndexByte(tag, ','); i >= 0 {
			name, opts = tag[:i], tag[i+1:]
		}
		fv := v.Field(i)
		// An embedded struct with no json name is inlined, matching encoding/json.
		if f.Anonymous && name == "" && fv.Kind() == reflect.Struct {
			flattenStruct(fv, out)
			continue
		}
		if name == "" {
			name = f.Name
		}
		if strings.Contains(opts, "omitempty") && isEmpty(fv) {
			continue
		}
		out[name] = sanitize(fv)
	}
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		// A NaN is "not computed", which for an omitempty field means omit it
		// rather than emit an explicit null.
		return v.Float() == 0 || math.IsNaN(v.Float())
	case reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	}
	return false
}

func keyString(k reflect.Value) string {
	if k.Kind() == reflect.String {
		return k.String()
	}
	b, _ := json.Marshal(k.Interface())
	return strings.Trim(string(b), `"`)
}
