// Package logparse reads server access logs in CSV form into typed events.
//
// The expected format is a header row followed by records of the form
//
//	timestamp,username,operation,size
//
// where timestamp uses time.UnixDate layout, operation is "upload" or
// "download", and size is a non-negative integer number of kilobytes.
package logparse

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Operation is the kind of file transfer recorded by an event.
type Operation string

// Supported operations.
const (
	// Upload is a file sent to the server.
	Upload Operation = "upload"
	// Download is a file retrieved from the server.
	Download Operation = "download"
)

// Event is a single parsed log record.
type Event struct {
	// Time is when the operation occurred.
	Time time.Time
	// User is the username that performed the operation.
	User string
	// Op is the operation performed.
	Op Operation
	// SizeKB is the transferred size in kilobytes.
	SizeKB int
}

const numFields = 4

// Parse reads a CSV log from r and returns its events in file order.
// The first record is treated as a header and skipped. Any malformed record
// causes Parse to return an error identifying its line number.
func Parse(r io.Reader) ([]Event, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = numFields
	cr.ReuseRecord = true

	if _, err := cr.Read(); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading header: %w", err)
	}

	events := make([]Event, 0, 1024)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			// csv.ParseError already carries the line number.
			return nil, err
		}
		ev, err := parseRecord(rec)
		if err != nil {
			line, _ := cr.FieldPos(0)
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		events = append(events, ev)
	}
}

// parseRecord converts one CSV record (timestamp, username, operation, size)
// into an Event.
func parseRecord(rec []string) (Event, error) {
	t, err := time.Parse(time.UnixDate, rec[0])
	if err != nil {
		return Event{}, fmt.Errorf("invalid timestamp %q: %w", rec[0], err)
	}
	op := Operation(rec[2])
	if op != Upload && op != Download {
		return Event{}, fmt.Errorf("unknown operation %q", rec[2])
	}
	size, err := strconv.Atoi(rec[3])
	if err != nil {
		return Event{}, fmt.Errorf("invalid size %q: %w", rec[3], err)
	}
	if size < 0 {
		return Event{}, fmt.Errorf("negative size %d", size)
	}
	return Event{Time: t, User: rec[1], Op: op, SizeKB: size}, nil
}
