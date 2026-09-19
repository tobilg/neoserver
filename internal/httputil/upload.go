package httputil

import (
	"context"
	"io"
	"net/http"
	"time"
)

// UploadRequest extends only an authenticated upload's transport deadlines.
// The hard budget remains bounded even if a client continuously trickles bytes.
func UploadRequest(w http.ResponseWriter, r *http.Request, total, idle time.Duration) (*http.Request, context.CancelFunc) {
	if total <= 0 {
		total = 15 * time.Minute
	}
	if idle <= 0 {
		idle = time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), total)
	deadline, _ := ctx.Deadline()
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(deadline.Add(30 * time.Second))
	body := &uploadBody{ReadCloser: r.Body, controller: controller, ctx: ctx, deadline: deadline, idle: idle}
	r = r.WithContext(ctx)
	r.Body = body
	return r, cancel
}

type uploadBody struct {
	io.ReadCloser
	controller *http.ResponseController
	ctx        context.Context
	deadline   time.Time
	idle       time.Duration
}

func (b *uploadBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(b.idle)
	if deadline.After(b.deadline) {
		deadline = b.deadline
	}
	_ = b.controller.SetReadDeadline(deadline)
	return b.ReadCloser.Read(p)
}
