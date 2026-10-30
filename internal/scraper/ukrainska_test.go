package scraper

import "testing"

func TestCleanUkrainskaLocation(t *testing.T) {
	tests := []struct{ input, want string }{
		{"u m.Södertälje.", "Södertälje"},
		{"у м. Södertälje", "Södertälje"},
		{"Södertälje", "Södertälje"},
		{"Nygatan 2, 151 72 Södertälje", "Nygatan 2, 151 72 Södertälje"},
		{"Mariakyrkan", "Mariakyrkan"},
	}
	for _, tt := range tests {
		if got := cleanUkrainskaLocation(tt.input); got != tt.want {
			t.Errorf("cleanUkrainskaLocation(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
