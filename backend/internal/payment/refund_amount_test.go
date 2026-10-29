package payment

import (
	"math"
	"testing"
)

func TestCalculateRefundAmount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		balance        float64
		rebate         float64
		feeRatePercent float64
		wantBase       float64
		wantFee        float64
		wantRefundable float64
	}{
		{
			name:           "整笔余额无返利",
			balance:        100,
			rebate:         0,
			feeRatePercent: 3,
			wantBase:       100,
			wantFee:        3,
			wantRefundable: 97,
		},
		{
			name:           "先扣返利再收手续费",
			balance:        100,
			rebate:         20,
			feeRatePercent: 3,
			wantBase:       80,
			wantFee:        2.4,
			wantRefundable: 77.6,
		},
		{
			name:           "零头向下截断且手续费向上取整",
			balance:        100.456,
			rebate:         0,
			feeRatePercent: 3,
			wantBase:       100.45,
			wantFee:        3.02,
			wantRefundable: 97.43,
		},
		{
			name:           "返利等于余额时无款可退",
			balance:        50,
			rebate:         50,
			feeRatePercent: 3,
			wantBase:       0,
			wantFee:        0,
			wantRefundable: 0,
		},
		{
			name:           "返利超过余额按零处理",
			balance:        10,
			rebate:         25,
			feeRatePercent: 3,
			wantBase:       0,
			wantFee:        0,
			wantRefundable: 0,
		},
		{
			name:           "费率为零全额可退",
			balance:        88.88,
			rebate:         8.88,
			feeRatePercent: 0,
			wantBase:       80,
			wantFee:        0,
			wantRefundable: 80,
		},
		{
			name:           "负数余额按零处理",
			balance:        -5,
			rebate:         0,
			feeRatePercent: 3,
			wantBase:       0,
			wantFee:        0,
			wantRefundable: 0,
		},
		{
			name:           "百分之一百费率最多扣光基数",
			balance:        10,
			rebate:         0,
			feeRatePercent: 100,
			wantBase:       10,
			wantFee:        10,
			wantRefundable: 0,
		},
		{
			name:           "极小基数只留一分",
			balance:        0.01,
			rebate:         0,
			feeRatePercent: 3,
			wantBase:       0.01,
			wantFee:        0.01,
			wantRefundable: 0,
		},
		{
			name:           "大额不丢精度",
			balance:        12345678.91,
			rebate:         345678.91,
			feeRatePercent: 3,
			wantBase:       12000000,
			wantFee:        360000,
			wantRefundable: 11640000,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CalculateRefundAmount(tc.balance, tc.rebate, tc.feeRatePercent)
			if got.Base != tc.wantBase {
				t.Errorf("Base = %v, want %v", got.Base, tc.wantBase)
			}
			if got.Fee != tc.wantFee {
				t.Errorf("Fee = %v, want %v", got.Fee, tc.wantFee)
			}
			if got.Refundable != tc.wantRefundable {
				t.Errorf("Refundable = %v, want %v", got.Refundable, tc.wantRefundable)
			}
			// 恒等式：可退款金额必须严格等于基数减手续费，否则前端与账单对不上。
			if diff := got.Base - got.Fee - got.Refundable; math.Abs(diff) > 1e-9 {
				t.Errorf("Base - Fee - Refundable = %v, want 0", diff)
			}
		})
	}
}

func TestCalculateRefundAmountRejectsNonFiniteInput(t *testing.T) {
	t.Parallel()

	for _, balance := range []float64{math.NaN(), math.Inf(-1), math.Inf(1)} {
		got := CalculateRefundAmount(balance, 0, 3)
		if math.IsNaN(got.Refundable) || math.IsInf(got.Refundable, 0) {
			t.Fatalf("balance %v produced non-finite refundable %v", balance, got.Refundable)
		}
	}
	got := CalculateRefundAmount(math.NaN(), math.Inf(1), math.NaN())
	if got != (RefundAmount{}) {
		t.Errorf("non-finite inputs should collapse to zero, got %+v", got)
	}
}
