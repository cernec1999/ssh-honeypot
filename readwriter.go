package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

func sanitize(s string) string {
	s = strings.Replace(s, "\r", "", -1)
	s = strings.Replace(s, "\n", "<br/>", -1)
	s = strings.Replace(s, "'", "\\'", -1)
	s = strings.Replace(s, "\b", "<backspace>", -1)
	return s
}

// NewSQLReadCloser creates a new SqlReadCloser struct
func NewSQLReadCloser(r io.ReadCloser) io.ReadCloser {
	return &SQLReadCloser{ReadCloser: r}
}

// SQLReadCloser type to export into a DB
type SQLReadCloser struct {
	io.ReadCloser
	buffer bytes.Buffer
}

func (sq *SQLReadCloser) Read(p []byte) (n int, err error) {
	n, err = sq.ReadCloser.Read(p)
	sq.buffer.WriteString(sanitize(string(p[:n])))
	return n, err
}

func (sq *SQLReadCloser) String() string {
	return sq.buffer.String()
}

// Close the connection
func (sq *SQLReadCloser) Close() error {
	fmt.Println(sq.buffer.String())
	return sq.ReadCloser.Close()
}
