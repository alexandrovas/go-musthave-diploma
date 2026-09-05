package luhn

import "testing"

func TestIsValid(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{"empty string", "", false},
		{"single zero", "0", true},
		{"single non-zero digit", "4", false},
		{"valid visa", "4111111111111111", true},
		{"valid mastercard", "5500000000000004", true},
		{"invalid check digit", "4111111111111112", false},
		{"valid order number from spec", "9278923470", true},
		{"valid order number from spec 2", "12345678903", true},
		{"valid order number from spec 3", "346436439", true},
		{"valid order number from spec 4", "79927398713", true},
		{"invalid non-digit", "1234a678", false},
		{"all same digits invalid", "1111111111111111", false},
		{"with letters", "abc", false},
		{"with spaces", "123 456", false},
		{"two digits valid", "42", true},
		{"two digits invalid", "43", false},
		{"withdrawal order from spec", "2377225624", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.number); got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.number, got, tt.want)
			}
		})
	}
}
