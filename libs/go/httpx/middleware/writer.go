// Copyright 2026 Mindclade. All rights reserved.
// Confidential, proprietary, and trade-secret information.

package middleware

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
)

type trackingWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (writer *trackingWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *trackingWriter) Status() int {
	if writer.status == 0 {
		return http.StatusOK
	}
	return writer.status
}
func (writer *trackingWriter) Bytes() int64    { return writer.bytes }
func (writer *trackingWriter) Committed() bool { return writer.status != 0 }

func (writer *trackingWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *trackingWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	count, err := writer.ResponseWriter.Write(value)
	writer.bytes += int64(count)
	return count, err
}
func (writer *trackingWriter) ReadFrom(reader io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom); ok {
		count, err := readerFrom.ReadFrom(reader)
		writer.bytes += count
		return count, err
	}
	count, err := io.Copy(struct{ io.Writer }{writer}, reader)
	return count, err
}
func (writer *trackingWriter) Flush() {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}
func (writer *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httpx: response writer does not support hijacking")
	}
	return hijacker.Hijack()
}
func (writer *trackingWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := writer.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}
