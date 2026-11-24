package tun

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpen(t *testing.T) {
	d, err := Open("nmotest%d", 1500)
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
		t.Skip("needs CAP_NET_ADMIN:", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.Name(), "nmotest") || strings.Contains(d.Name(), "%") {
		t.Fatalf("name = %q", d.Name())
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.Read(make([]byte, 2000))
		done <- err
	}()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("Read returned nil after Close")
	}
}
