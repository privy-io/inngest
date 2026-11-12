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
		wantErr bool
	}{
		{name: "default", in: "30d", want: 30 * day},
		{name: "days", in: "7d", want: 7 * day},
		{name: "go duration", in: "720h", want: 720 * time.Hour},
		{name: "unparseable days", in: "xd", wantErr: true},
		{name: "unparseable duration", in: "soon", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRetention(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
