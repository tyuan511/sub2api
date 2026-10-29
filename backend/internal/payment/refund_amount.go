package payment

import (
	"math"

	"github.com/shopspring/decimal"
)

// RefundFractionDigits 是退款金额的展示与结算精度：账户余额在计费时保留 8 位小数，
// 但退款只能按最小货币单位（2 位）结清，多出来的零头留在用户余额里继续可用。
const RefundFractionDigits = 2

// RefundAmount 是一笔余额退款的金额拆解。
//
//	Base       参与退款的基数 = 余额 − 已到账返利（截断到 2 位）
//	Fee        手续费 = 基数 × 费率，向上取整到 2 位（平台不亏零头）
//	Refundable 可退款金额 = 基数 − 手续费，恒等于 2 位精度下的差值
//
// 三者始终满足 Refundable = Base − Fee，前端与账单核对时不会出现第四种口径。
type RefundAmount struct {
	Base       float64
	Fee        float64
	Refundable float64
}

// CalculateRefundAmount 按「(余额 − 已到账返利) × (1 − 费率)」计算可退款金额。
//
// 舍入方向是刻意选择的：基数向下截断、手续费向上取整，两个方向都偏保守，
// 保证退给用户的钱不会多于应退金额。入参非法（NaN/Inf/负数）按 0 处理。
func CalculateRefundAmount(balance, rebateAmount, feeRatePercent float64) RefundAmount {
	base := toFiniteNonNegativeDecimal(balance).Sub(toFiniteNonNegativeDecimal(rebateAmount))
	if base.IsNegative() {
		base = decimal.Zero
	}
	base = base.Truncate(RefundFractionDigits)

	rate := toFiniteNonNegativeDecimal(feeRatePercent)
	if rate.IsNegative() {
		rate = decimal.Zero
	}
	fee := base.Mul(rate).Div(decimal.NewFromInt(100)).RoundUp(RefundFractionDigits)
	if fee.GreaterThan(base) {
		fee = base
	}
	refundable := base.Sub(fee)

	return RefundAmount{
		Base:       base.Round(RefundFractionDigits).InexactFloat64(),
		Fee:        fee.Round(RefundFractionDigits).InexactFloat64(),
		Refundable: refundable.Round(RefundFractionDigits).InexactFloat64(),
	}
}

func toFiniteNonNegativeDecimal(value float64) decimal.Decimal {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return decimal.Zero
	}
	return decimal.NewFromFloat(value)
}
