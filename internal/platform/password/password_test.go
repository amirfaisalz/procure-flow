package password_test

import (
	"strings"
	"testing"

	"github.com/amirfaisalz/procure-flow/internal/platform/password"
)

func TestHashAndVerify(t *testing.T) {
	hasher := password.New(4) // bcrypt.MinCost keeps the test fast

	hash, err := hasher.Hash("correct-horse")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("Hash() = %q, want a bcrypt hash", hash)
	}
	if err := hasher.Verify(hash, "correct-horse"); err != nil {
		t.Errorf("Verify(correct) error = %v", err)
	}
	if err := hasher.Verify(hash, "wrong-horse"); err == nil {
		t.Error("Verify(wrong) error = nil, want failure")
	}
}

func TestHashesAreSalted(t *testing.T) {
	hasher := password.New(4)

	first, err := hasher.Hash("same-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	second, err := hasher.Hash("same-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password are equal; salt is missing")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "too short", input: "short", wantErr: true},
		{name: "minimum", input: "12345678"},
		{name: "too long", input: strings.Repeat("a", password.MaxBytes+1), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := password.Validate(test.input)
			if (err != nil) != test.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDummyHashNeverVerifies(t *testing.T) {
	hasher := password.New(4)
	dummy := hasher.DummyHash()

	if dummy == "" {
		t.Fatal("DummyHash() is empty")
	}
	if err := hasher.Verify(dummy, "anything-at-all"); err == nil {
		t.Error("Verify(dummy) succeeded, want failure")
	}
	if dummy != hasher.DummyHash() {
		t.Error("DummyHash() is not stable across calls")
	}
}

func TestDefaultCost(t *testing.T) {
	if got := password.NewDefault().Cost(); got != password.DefaultCost {
		t.Errorf("Cost() = %d, want %d", got, password.DefaultCost)
	}
	if got := password.New(999).Cost(); got != password.DefaultCost {
		t.Errorf("New(999).Cost() = %d, want default %d", got, password.DefaultCost)
	}
}
