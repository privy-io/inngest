package cleanup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRetention(t *testing.T) {
	day := 24 * time.Hour

	tests := []struct {
		name    string
		in      string
		want    time.Duration
		dryRun  bool
		wantErr bool
	}{
		{name: "default", in: "30d", want: 30 * day},
		{name: "days", in: "7d", want: 7 * day},
		{name: "go duration", in: "720h", want: 720 * time.Hour},
		{name: "dry run days", in: "dry-run:7d", want: 7 * day, dryRun: true},
		{name: "dry run duration", in: "dry-run:12h", want: 12 * time.Hour, dryRun: true},
		{name: "unparseable days", in: "xd", wantErr: true},
		{name: "unparseable duration", in: "soon", wantErr: true},
		{name: "dry run with bad value", in: "dry-run:xd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dryRun, err := parseRetention(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.dryRun, dryRun)
		})
	}
}
