package contracts

import "encoding/json"

// Optional distinguishes an absent property from a present zero or explicit null.
// Generated fields use Go 1.24's omitzero and this IsZero method.
type Optional[T any] struct {
	Value   T
	Present bool
}

func Some[T any](value T) Optional[T]              { return Optional[T]{Value: value, Present: true} }
func (o Optional[T]) IsZero() bool                 { return !o.Present }
func (o Optional[T]) MarshalJSON() ([]byte, error) { return EncodeJSON(o.Value) }
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = value
	o.Present = true
	return nil
}
