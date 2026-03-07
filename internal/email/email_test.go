package email

import "testing"

func TestNormalizeNewlines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare LF", "a\nb\nc", "a\r\nb\r\nc"},
		{"bare CR", "a\rb\rc", "a\r\nb\r\nc"},
		{"CRLF passthrough", "a\r\nb\r\nc", "a\r\nb\r\nc"},
		{"mixed", "a\r\nb\nc\rd", "a\r\nb\r\nc\r\nd"},
		{"empty", "", ""},
		{"no newlines", "hello world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeNewlines(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeNewlines(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
