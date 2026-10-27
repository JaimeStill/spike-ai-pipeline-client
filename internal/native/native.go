// Package native holds what the module's native tier shares across its layers: the check of a
// request's Native field, and the encoding of a wire body that merges it, which protocol, the
// formats, and the client's operations outside the Format each need and none can import from
// another.
package native

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Valid reports whether v can be a request's Native field: empty, or a JSON object with unique
// member names.
func Valid(v jsontext.Value) bool {
	return len(v) == 0 || (v.Kind() == jsontext.KindBeginObject && v.IsValid())
}

// Marshal encodes body, a wire body whose Native field is embedded in its top level, as JSON. A
// Native member named like one body's typed fields set is a duplicate name, which it reports as
// a clash naming the member. body's Native must be Valid, or a repeated member within it is
// reported as a clash too.
func Marshal(body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		var se *jsontext.SyntacticError
		if errors.As(err, &se) && errors.Is(se.Err, jsontext.ErrDuplicateName) {
			return nil, fmt.Errorf("native member %s clashes with a member the request sets", se.JSONPointer.LastToken())
		}
		return nil, fmt.Errorf("encode request: %w", err)
	}
	return b, nil
}
