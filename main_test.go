package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Setup logic here

	code := m.Run()

	// Teardown logic here

	os.Exit(code)
}

func TestHelloWorld(t *testing.T) {
	t.Log("hello world!")
}
