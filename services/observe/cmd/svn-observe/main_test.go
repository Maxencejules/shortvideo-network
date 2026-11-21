package main

import "testing"

func TestLoopbackOnly(t *testing.T) {
	if !loopback("127.0.0.1:9") || !loopback("[::1]:9") {
		t.Fatal("loopback addresses were refused")
	}
	if loopback("0.0.0.0:9") || loopback("10.0.0.1:9") || loopback(":9") {
		t.Fatal("a non-loopback address was accepted")
	}
}
