package providers

import "testing"

func TestDecodeBolt11Amount(t *testing.T) {
	tests := []struct {
		name    string
		invoice string
		want    int64
		wantErr bool
	}{
		{
			name:    "100 micro-BTC (10000 sats)",
			invoice: "lnbc100u1pjexample",
			want:    10000,
		},
		{
			name:    "10 micro-BTC (1000 sats)",
			invoice: "lnbc10u1pjexample",
			want:    1000,
		},
		{
			name:    "1 milli-BTC (100000 sats)",
			invoice: "lnbc1m1pjexample",
			want:    100000,
		},
		{
			name:    "50 micro-BTC (5000 sats)",
			invoice: "lnbc50u1pjexample",
			want:    5000,
		},
		{
			name:    "250 nano-BTC (25 sats)",
			invoice: "lnbc250n1pjexample",
			want:    25,
		},
		{
			name:    "testnet invoice",
			invoice: "lntb100u1pjexample",
			want:    10000,
		},
		{
			name:    "regtest invoice",
			invoice: "lnbcrt100u1pjexample",
			want:    10000,
		},
		{
			name:    "invalid prefix",
			invoice: "xyz100u1pjexample",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBolt11Amount(tt.invoice)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
