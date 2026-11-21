package service

import (
	"net/http"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/observe/client"
)

func TestRealUploadsStopAtTheAdmissionCap(t *testing.T) {
	base, token, stop, err := client.Listen(map[string]int64{
		client.KindUploads: 2, client.KindOutputBytes: 100000, client.KindProcessing: 100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	h := newHarnessGate(t, wideLimits(1_000_000), nil, client.New(base, token))
	first, _ := h.intent(h.a.Token, "up-1", 10, nil)
	replay, _ := h.intent(h.a.Token, "up-1", 10, nil)
	second, _ := h.intent(h.a.Token, "up-2", 10, nil)
	third, thirdBody := h.intent(h.a.Token, "up-3", 10, nil)
	if first != http.StatusAccepted || replay != http.StatusAccepted || second != http.StatusAccepted {
		t.Fatalf("open %d %d %d", first, replay, second)
	}
	if third != http.StatusTooManyRequests || errorCode(t, thirdBody) != "ADMISSION" {
		t.Fatalf("cap %d %#v", third, thirdBody)
	}

	outBase, outToken, outStop, err := client.Listen(map[string]int64{
		client.KindUploads: 100, client.KindOutputBytes: 1000, client.KindProcessing: 100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(outStop)
	out := newHarnessGate(t, wideLimits(1_000_000), nil, client.New(outBase, outToken))
	one, _ := out.intent(out.a.Token, "bytes-1", 10, nil)
	again, _ := out.intent(out.a.Token, "bytes-1", 10, nil)
	two, twoBody := out.intent(out.a.Token, "bytes-2", 10, nil)
	if one != http.StatusAccepted || again != http.StatusAccepted {
		t.Fatalf("output open %d %d", one, again)
	}
	if two != http.StatusTooManyRequests || errorCode(t, twoBody) != "ADMISSION" {
		t.Fatalf("output cap %d %#v", two, twoBody)
	}

	down := newHarnessGate(t, wideLimits(1_000_000), nil, client.New("http://127.0.0.1:1", token))
	status, body := down.intent(down.a.Token, "down-1", 10, nil)
	if status != http.StatusServiceUnavailable || errorCode(t, body) != "ADMISSION_UNREACHABLE" {
		t.Fatalf("down %d %#v", status, body)
	}
}
