package main

import (
	"strings"
	"testing"
)

func TestReadResetPassword(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "newline terminated", input: "password-with-symbols!\n", want: "password-with-symbols!"},
		{name: "CRLF terminated", input: "password-with-symbols!\r\n", want: "password-with-symbols!"},
		{name: "missing newline", input: "password-with-symbols!", want: "password-with-symbols!"},
		{name: "empty input", input: "", wantErr: true},
		{name: "empty line", input: "\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readResetPassword(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("readResetPassword() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("readResetPassword() = %q, want %q", got, tt.want)
			}
		})
	}
}
