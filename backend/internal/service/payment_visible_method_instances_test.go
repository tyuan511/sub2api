package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestEnabledVisibleMethodsForProviderBepusdt(t *testing.T) {
	tests := []struct {
		name          string
		supportedType string
		want          []string
	}{
		{name: "empty supported types use provider default", want: []string{payment.TypeUsdt}},
		{name: "explicit usdt type", supportedType: "usdt", want: []string{payment.TypeUsdt}},
		{name: "legacy bepusdt type normalizes to usdt", supportedType: "bepusdt", want: []string{payment.TypeUsdt}},
		{name: "unsupported type is hidden", supportedType: "alipay", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enabledVisibleMethodsForProvider(payment.TypeBepusdt, tt.supportedType)
			if len(got) != len(tt.want) {
				t.Fatalf("enabledVisibleMethodsForProvider() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("enabledVisibleMethodsForProvider() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
