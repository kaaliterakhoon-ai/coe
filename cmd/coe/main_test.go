package main

import "testing"

func TestParseServeOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "default",
			args: nil,
			want: "",
		},
		{
			name: "debug override",
			args: []string{"--log-level", "debug"},
			want: "debug",
		},
		{
			name:    "invalid level",
			args:    []string{"--log-level", "trace"},
			wantErr: true,
		},
		{
			name:    "extra args",
			args:    []string{"extra"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseServeOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseServeOptions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseServeOptions() error = %v", err)
			}
			if got.LogLevel != tt.want {
				t.Fatalf("parseServeOptions().LogLevel = %q, want %q", got.LogLevel, tt.want)
			}
		})
	}
}
