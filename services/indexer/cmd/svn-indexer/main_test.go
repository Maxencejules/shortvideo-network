package main

import (
	"bytes"
	"context"
	"testing"
)

func TestUsageAndEnvironment(t *testing.T) {
	var errBuf bytes.Buffer
	if code := run(context.Background(), nil, nil, &errBuf, &errBuf); code != 2 {
		t.Fatalf("usage %d", code)
	}
	errBuf.Reset()
	if code := run(context.Background(), []string{"status", "--config", "missing"}, []string{"PQGO_DEBUG=1"}, &errBuf, &errBuf); code != 3 {
		t.Fatalf("environment %d %s", code, errBuf.String())
	}
	if !bytes.Contains(errBuf.Bytes(), []byte("PQGO_DEBUG")) {
		t.Fatalf("diagnostic %s", errBuf.String())
	}
}
