package main

import (
	"io"
	"time"
)

// NewSQLReadCloser creates a new SqlReadCloser struct
func NewSQLReadCloser(r io.ReadCloser, sql SQLHoneypotDBConnection) io.ReadCloser {
	return &SQLReadCloser{ReadCloser: r, sql: sql, prevTime: time.Now()}
}

// SQLReadCloser type to export into a DB
type SQLReadCloser struct {
	io.ReadCloser
	sql      SQLHoneypotDBConnection
	prevTime time.Time
}

// Read reads in some bytes and puts it in the database
func (sq *SQLReadCloser) Read(p []byte) (n int, err error) {
	// read in the bytes
	n, err = sq.ReadCloser.Read(p)

	// We may have a closed connection here
	if err != nil {
		return 0, err
	}

	// calculate time delay from last command
	curTime := time.Now()
	delay := curTime.Sub(sq.prevTime).Milliseconds()
	sq.prevTime = curTime

	// write to SQL
	err = sq.sql.InsertMetadata(p[:n], delay)
	return n, err
}

// Close the connection
func (sq *SQLReadCloser) Close() error {
	return sq.ReadCloser.Close()
}
