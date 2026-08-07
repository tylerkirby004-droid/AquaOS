package core

import "testing"

func TestNewIDProducesValidUniqueIDs(t *testing.T) {
	first, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("NewID returned duplicate values")
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestIDValidateRejectsMalformedValue(t *testing.T) {
	if err := ID("not-a-uuid").Validate(); err == nil {
		t.Fatal("Validate() expected error")
	}
}
