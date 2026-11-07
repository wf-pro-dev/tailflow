package fakesource

import "encoding/json"

// jsonMarshal is a thin wrapper that panics on error. It is only used to
// serialise canned test fixtures, where a marshalling error is a test-authoring
// bug rather than a runtime condition.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// jsonMarshalInto marshals v and unmarshals into out. Used by Backend.Do to
// populate a typed out parameter from a canned Go value.
func jsonMarshalInto(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// jsonUnmarshalInto unmarshals raw bytes into out.
func jsonUnmarshalInto(raw []byte, out any) error {
	return json.Unmarshal(raw, out)
}
