package main

import (
	"io"
)

// NewSQLReadCloser creates a new SqlReadCloser struct
func NewSQLReadCloser(r io.ReadCloser, sql SQLHoneypotDBConnection) io.ReadCloser {
	return &SQLReadCloser{ReadCloser: r, sql: sql}
}

// SQLReadCloser type to export into a DB
type SQLReadCloser struct {
	io.ReadCloser
	sql SQLHoneypotDBConnection
}

func (sq *SQLReadCloser) Read(p []byte) (n int, err error) {
	n, err = sq.ReadCloser.Read(p)
	// write to SQL
	err = sq.sql.InsertMetadata(p[:n])
	return n, err
}

// Close the connection
func (sq *SQLReadCloser) Close() error {
	return sq.ReadCloser.Close()
}
