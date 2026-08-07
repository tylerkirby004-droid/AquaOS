package domain

import "errors"

// ValueKind identifies the one populated member of Value.
type ValueKind string

//nolint:revive // ValueKind documents the closed set in this block.
const (
	ValueQuantity ValueKind = "quantity"
	ValueBoolean  ValueKind = "boolean"
	ValueText     ValueKind = "text"
)

// Value is a strongly typed canonical scalar. Exactly one member must be set.
type Value struct {
	Kind     ValueKind `json:"kind"`
	Quantity *Quantity `json:"quantity,omitempty"`
	Boolean  *bool     `json:"boolean,omitempty"`
	Text     *string   `json:"text,omitempty"`
}

// NewQuantityValue constructs a canonical quantity value.
func NewQuantityValue(value Quantity) Value { return Value{Kind: ValueQuantity, Quantity: &value} }

// NewBooleanValue constructs a canonical boolean value.
func NewBooleanValue(value bool) Value { return Value{Kind: ValueBoolean, Boolean: &value} }

// NewTextValue constructs a canonical text value.
func NewTextValue(value string) Value { return Value{Kind: ValueText, Text: &value} }

// Validate ensures the discriminator and populated member agree exactly.
func (v Value) Validate() error {
	count := 0
	if v.Quantity != nil {
		count++
	}
	if v.Boolean != nil {
		count++
	}
	if v.Text != nil {
		count++
	}
	if count != 1 {
		return errors.New("canonical value must contain exactly one member")
	}
	switch v.Kind {
	case ValueQuantity:
		if v.Quantity == nil {
			return errors.New("quantity value is required")
		}
		return v.Quantity.Validate()
	case ValueBoolean:
		if v.Boolean == nil {
			return errors.New("boolean value is required")
		}
	case ValueText:
		if v.Text == nil {
			return errors.New("text value is required")
		}
	default:
		return errors.New("unsupported canonical value kind")
	}
	return nil
}

// Clone returns a pointer-independent value copy.
func (v Value) Clone() Value {
	result := v
	if v.Quantity != nil {
		item := *v.Quantity
		result.Quantity = &item
	}
	if v.Boolean != nil {
		item := *v.Boolean
		result.Boolean = &item
	}
	if v.Text != nil {
		item := *v.Text
		result.Text = &item
	}
	return result
}
