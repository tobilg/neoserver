package httputil

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// Exercise real socket deadlines through the wrappers used by the server.
func TestUploadOutlivesNormalReadTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r, cancel := UploadRequest(w, r, time.Second, 200*time.Millisecond)
		defer cancel()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("upload failed: %v", err)
			w.WriteHeader(408)
			return
		}
		_, _ = w.Write(body)
	})
	srv := httptest.NewUnstartedServer(middleware.Compress(5)(handler))
	srv.Config.ReadTimeout = 30 * time.Millisecond
	srv.Config.WriteTimeout = 30 * time.Millisecond
	srv.Start()
	defer srv.Close()
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		for i := 0; i < 4; i++ {
			_, _ = writer.Write([]byte("x"))
			time.Sleep(50 * time.Millisecond)
		}
	}()
	resp, err := srv.Client().Post(srv.URL, "application/octet-stream", reader)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "xxxx" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
}

func TestUploadIdleAndTotalBudgets(t *testing.T) {
	for _, tc := range []struct {
		name        string
		total, idle time.Duration
		trickle     bool
	}{
		{"idle", time.Second, 60 * time.Millisecond, false}, {"total", 120 * time.Millisecond, 100 * time.Millisecond, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := make(chan error, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r, cancel := UploadRequest(w, r, tc.total, tc.idle)
				defer cancel()
				_, err := io.Copy(io.Discard, r.Body)
				result <- err
				w.WriteHeader(408)
			}))
			defer srv.Close()
			conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_, _ = fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10000\r\n\r\nx")
			done := make(chan struct{})
			defer close(done)
			if tc.trickle {
				go func() {
					ticker := time.NewTicker(20 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-done:
							return
						case <-ticker.C:
							_, _ = conn.Write([]byte("x"))
						}
					}
				}()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("incomplete upload accepted")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("upload deadline did not fire")
			}
		})
	}
}
