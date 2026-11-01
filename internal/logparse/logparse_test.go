package logparse

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

const header = "timestamp,username,operation,size\n"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Event
		wantErr string // substring expected in the error; empty means no error
	}{
		{
			name: "valid rows",
			input: header +
				"Sun Apr 12 22:10:38 UTC 2020,sarah94,download,34\n" +
				"Wed Apr 15 08:00:00 UTC 2020,jeff22,upload,0\n",
			want: []Event{
				{Time: time.Date(2020, 4, 12, 22, 10, 38, 0, time.UTC), User: "sarah94", Op: Download, SizeKB: 34},
				{Time: time.Date(2020, 4, 15, 8, 0, 0, 0, time.UTC), User: "jeff22", Op: Upload, SizeKB: 0},
			},
		},
		{name: "header only", input: header, want: []Event{}},
		{
			name:    "bad timestamp",
			input:   header + "2020-04-12,sarah94,download,34\n",
			wantErr: "line 2: invalid timestamp",
		},
		{
			name:    "bad size",
			input:   header + "Sun Apr 12 22:10:38 UTC 2020,sarah94,download,big\n",
			wantErr: "line 2: invalid size",
		},
		{
			name:    "negative size",
			input:   header + "Sun Apr 12 22:10:38 UTC 2020,sarah94,download,-1\n",
			wantErr: "line 2: negative size",
		},
		{
			name: "unknown operation",
			input: header +
				"Sun Apr 12 22:10:38 UTC 2020,sarah94,download,34\n" +
				"Sun Apr 12 22:10:38 UTC 2020,sarah94,delete,34\n",
			wantErr: "line 3: unknown operation",
		},
		{
			name:    "wrong field count",
			input:   header + "Sun Apr 12 22:10:38 UTC 2020,sarah94,download\n",
			wantErr: "line 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseServerLog(t *testing.T) {
	f, err := os.Open("../../server_log.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	events, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(events) != 657 {
		t.Errorf("len(events) = %d, want 657", len(events))
	}
}
