package api

import (
	"bytes"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
)

func TestTerminalOriginAllowed(t *testing.T) {
	tests := []struct {
		name    string
		allowed string
		origin  string
		want    bool
	}{
		{name: "same origin", allowed: "http://localhost:5173", origin: "http://localhost:5173", want: true},
		{name: "loopback development aliases", allowed: "http://localhost:5173", origin: "http://127.0.0.1:5173", want: true},
		{name: "different port", allowed: "http://localhost:5173", origin: "http://127.0.0.1:4173", want: false},
		{name: "different host", allowed: "http://localhost:5173", origin: "https://example.invalid", want: false},
		{name: "wildcard", allowed: "*", origin: "https://example.invalid", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := terminalOriginAllowed(test.allowed, test.origin); got != test.want {
				t.Fatalf("terminalOriginAllowed(%q, %q) = %v, want %v", test.allowed, test.origin, got, test.want)
			}
		})
	}
}

func TestHandleTerminalClientMessage(t *testing.T) {
	input := &bytes.Buffer{}
	sizes := make(chan runtime.TerminalSize, 1)
	if err := handleTerminalClientMessage([]byte(`{"type":"input","data":"ls\r"}`), input, sizes); err != nil {
		t.Fatal(err)
	}
	if input.String() != "ls\r" {
		t.Fatalf("input = %q", input.String())
	}
	if err := handleTerminalClientMessage([]byte(`{"type":"resize","cols":100,"rows":30}`), input, sizes); err != nil {
		t.Fatal(err)
	}
	select {
	case size := <-sizes:
		if size.Columns != 100 || size.Rows != 30 {
			t.Fatalf("size = %#v", size)
		}
	default:
		t.Fatal("resize was not queued")
	}
	if err := handleTerminalClientMessage([]byte(`{"type":"resize","cols":0,"rows":30}`), input, sizes); err == nil {
		t.Fatal("expected invalid resize error")
	}
}
