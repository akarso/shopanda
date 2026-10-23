package returns

import (
	"math"
	"testing"
)

func TestAddRestockQty(t *testing.T) {
	tests := []struct {
		name    string
		onHand  int
		add     int
		want    int
		wantErr string
	}{
		{
			name:   "missing stock row is zero plus returned qty",
			onHand: 0,
			add:    4,
			want:   4,
		},
		{
			name:    "max int overflow",
			onHand:  math.MaxInt,
			add:     1,
			wantErr: "quantity overflow",
		},
		{
			name:    "add below one",
			onHand:  3,
			add:     0,
			wantErr: "quantity must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := addRestockQty(tt.onHand, tt.add)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("addRestockQty(%d, %d) err = %v, want %q", tt.onHand, tt.add, err, tt.wantErr)
				}
				if got != 0 {
					t.Fatalf("addRestockQty(%d, %d) = %d, want 0", tt.onHand, tt.add, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("addRestockQty(%d, %d): %v", tt.onHand, tt.add, err)
			}
			if got != tt.want {
				t.Fatalf("addRestockQty(%d, %d) = %d, want %d", tt.onHand, tt.add, got, tt.want)
			}
		})
	}
}
