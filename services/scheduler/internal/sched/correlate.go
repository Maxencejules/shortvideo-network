package sched

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// correlate sets X-Correlation-ID. The id is random. It is not an account
// identifier and it is not a job-queue depth.
func correlate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if !correlationOK(id) {
			var buf [16]byte
			if _, err := rand.Read(buf[:]); err != nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			id = "c" + hex.EncodeToString(buf[:])
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r)
	})
}

func correlationOK(id string) bool {
	if len(id) != 33 || id[0] != 'c' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
