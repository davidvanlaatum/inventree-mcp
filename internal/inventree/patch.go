package inventree

import "encoding/json"

type PatchFields map[string]PatchValue

type PatchValue struct {
	value any
}

func Set(value any) PatchValue {
	return PatchValue{value: value}
}

func Null() PatchValue {
	return PatchValue{value: nil}
}

func (f PatchFields) MarshalJSON() ([]byte, error) {
	payload := make(map[string]any, len(f))
	for key, value := range f {
		payload[key] = value.value
	}
	return json.Marshal(payload)
}
