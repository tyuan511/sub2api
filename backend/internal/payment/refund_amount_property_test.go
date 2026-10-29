package payment

import (
	"math"
	"math/rand"
	"testing"
)

// TestCalculateRefundAmountNeverExceedsBalance 锁住退款不会多退的核心不变量：
//
//	可退款金额 ≤ 可退基数 ≤ 账户余额（= 提交时冻结的金额）
//
// 只要这条成立，平台从账户里收走的钱（冻结额）永远 ≥ 实际退给用户的现金，
// 不存在"退了 100 只扣了 80"的亏损。随机输入覆盖浮点误差与极端比例。
func TestCalculateRefundAmountNeverExceedsBalance(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(20261023))
	for i := 0; i < 200000; i++ {
		balance := rng.Float64() * 100000
		rebate := rng.Float64() * balance * 1.2 // 故意允许返利大于余额
		rate := rng.Float64() * 100
		got := CalculateRefundAmount(balance, rebate, rate)

		if got.Refundable > got.Base+1e-9 {
			t.Fatalf("refundable %v > base %v (balance=%v rebate=%v rate=%v)", got.Refundable, got.Base, balance, rebate, rate)
		}
		if got.Base > balance+1e-9 {
			t.Fatalf("base %v > balance %v (rebate=%v rate=%v)", got.Base, balance, rebate, rate)
		}
		if got.Fee < 0 || got.Refundable < 0 {
			t.Fatalf("negative amount: %+v", got)
		}
		if math.Abs(got.Base-got.Fee-got.Refundable) > 1e-9 {
			t.Fatalf("base - fee != refundable: %+v", got)
		}
		// 只保留 2 位小数，多出来的精度会被下游当成分以下金额使用。
		if math.Abs(got.Refundable*100-math.Round(got.Refundable*100)) > 1e-6 {
			t.Fatalf("refundable not on a 0.01 grid: %v", got.Refundable)
		}
	}
}

// TestCalculateRefundAmountKeepsFeeNonNegative 保证任意费率下手续费都不会变成负数，
// 也就是不会因为"倒贴手续费"而退得比基数更多。
func TestCalculateRefundAmountKeepsFeeNonNegative(t *testing.T) {
	t.Parallel()

	for _, rate := range []float64{-10, 0, 0.5, 3, 33.33, 99.99, 100, 150, math.NaN(), math.Inf(1)} {
		got := CalculateRefundAmount(1000, 0, rate)
		if got.Fee < 0 {
			t.Fatalf("rate %v produced negative fee %v", rate, got.Fee)
		}
		if got.Refundable > 1000 {
			t.Fatalf("rate %v refunded more than the balance: %v", rate, got.Refundable)
		}
	}
}
