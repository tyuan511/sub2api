//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// refundMinForTests 是测试用的起退点；需要跨门槛的用例自己显式传值。
const refundMinForTests = 0.0

func TestNewBalanceRefundQuoteDeductsRebateBeforeFee(t *testing.T) {
	t.Parallel()

	// 余额 100，其中 20 是推广返利转进来的：先剔返利，再对 80 收 3%。
	quote := newBalanceRefundQuote(7, 100, 20, 3, refundMinForTests)
	require.Equal(t, int64(7), quote.UserID)
	require.Equal(t, 80.0, quote.RefundableBase)
	require.Equal(t, 20.0, quote.RebateAmount)
	require.Equal(t, 2.4, quote.FeeAmount)
	require.Equal(t, 77.6, quote.RefundableAmount)
	// 恒等式：可退款金额 = 可退基数 − 手续费，页面上四行数字必须自洽。
	require.InDelta(t, quote.RefundableBase-quote.FeeAmount, quote.RefundableAmount, 1e-9)
}

func TestNewBalanceRefundQuoteRoundsToTwoDecimals(t *testing.T) {
	t.Parallel()

	quote := newBalanceRefundQuote(1, 100.456, 0.005, 3, refundMinForTests)
	// 返利按 2 位收敛为 0.01，基数截断为 100.44，手续费向上取整为 3.02。
	require.Equal(t, 0.01, quote.RebateAmount)
	require.Equal(t, 100.44, quote.RefundableBase)
	require.Equal(t, 3.02, quote.FeeAmount)
	require.Equal(t, 97.42, quote.RefundableAmount)
	require.Equal(t, 100.456, quote.Balance)
}

func TestNewBalanceRefundQuoteKeepsRebateOnlyBalanceUnrefundable(t *testing.T) {
	t.Parallel()

	quote := newBalanceRefundQuote(1, 50, 50, 3, refundMinForTests)
	require.Equal(t, 0.0, quote.RefundableAmount)

	// 返利大于余额（历史数据或人为改库）也不该算出负数。
	quote = newBalanceRefundQuote(1, 10, 25, 3, refundMinForTests)
	require.Equal(t, 0.0, quote.RefundableBase)
	require.Equal(t, 0.0, quote.RefundableAmount)
}

// TestNewBalanceRefundQuoteAppliesMinimumThreshold 锁住起退点：可退金额低于门槛
// 就不给提交（前端隐藏入口只是体验，服务端才是约束）。
func TestNewBalanceRefundQuoteAppliesMinimumThreshold(t *testing.T) {
	t.Parallel()

	// 余额 100、无返利 → 可退 97，高于 $20 门槛，可以退。
	ok := newBalanceRefundQuote(1, 100, 0, 3, 20)
	require.False(t, ok.BelowMinimum)
	require.Equal(t, 97.0, ok.RefundableAmount)
	require.Equal(t, 20.0, ok.MinRefundAmount)

	// 余额 20 → 可退 19.40，低于门槛：不给退。
	below := newBalanceRefundQuote(1, 20, 0, 3, 20)
	require.True(t, below.BelowMinimum)
	require.Equal(t, 19.4, below.RefundableAmount)

	// 余额 25 但其中 10 是已到账返利 → 可退 14.50，同样被门槛挡住。
	rebate := newBalanceRefundQuote(1, 25, 10, 3, 20)
	require.True(t, rebate.BelowMinimum)

	// 恰好等于门槛放行，避免边界上"看得见按钮点不动"。
	exact := newBalanceRefundQuote(1, 20.62, 0, 3, 20)
	require.Equal(t, 20.0, exact.RefundableAmount)
	require.False(t, exact.BelowMinimum)

	// 门槛配成 0 表示不限制。
	none := newBalanceRefundQuote(1, 1, 0, 3, 0)
	require.False(t, none.BelowMinimum)
}

// TestBuildBalanceComposition 锁住余额构成折算：无法归因只取正差、金额一律 2 位。
func TestBuildBalanceComposition(t *testing.T) {
	t.Parallel()

	t.Run("归因超过余额时不产生负数无法归因", func(t *testing.T) {
		// 线上付了 1000，已经消费到只剩 10。
		got := buildBalanceComposition(1000, 0, 0, 0, 0, 10, 0)
		require.Equal(t, 1000.0, got.PaidOnline)
		require.Equal(t, 1000.0, got.AttributedTotal)
		require.Zero(t, got.Unattributed)
		require.Equal(t, 10.0, got.CurrentBalance)
	})

	t.Run("余额超出归因部分落到无法归因", func(t *testing.T) {
		// 线上 0、卡密 100、管理员加 18907.33，但账上有 19017.33 → 差 10 无来源。
		got := buildBalanceComposition(0, 100, 18907.33, 0, 0, 19017.33, 0)
		require.Zero(t, got.PaidOnline)
		require.Equal(t, 19007.33, got.AttributedTotal)
		require.Equal(t, 10.0, got.Unattributed)
	})

	t.Run("管理员扣款为负不会被当成来源", func(t *testing.T) {
		got := buildBalanceComposition(500, 0, -200, 0, 0, 300, 0)
		require.Equal(t, 300.0, got.AttributedTotal)
		require.Zero(t, got.Unattributed)
	})

	t.Run("冻结之后信号不能消失", func(t *testing.T) {
		// 申请时余额 110（其中 10 无来源），提交后整笔转入冻结：
		// 实时余额 0、冻结 110。构成必须仍然报出 10 的无法归因，
		// 否则管理员审核时恰好看不到最该看的那条信号。
		got := buildBalanceComposition(0, 30, 50, 0, 20, 0, 110)
		require.Equal(t, 100.0, got.AttributedTotal)
		require.Equal(t, 110.0, got.ReferenceBalance)
		require.Equal(t, 10.0, got.Unattributed)

		// 显式传申请快照时结果一致。
		got = buildBalanceComposition(0, 30, 50, 0, 20, 0, 110, 110)
		require.Equal(t, 10.0, got.Unattributed)
	})

	t.Run("精度收敛到两位", func(t *testing.T) {
		got := buildBalanceComposition(10.005, 0.333, 0, 0, 0, 20.999, 0)
		require.Equal(t, 10.01, got.PaidOnline)
		require.Equal(t, 0.33, got.RedeemCodes)
		require.Equal(t, 21.0, got.CurrentBalance)
		// 10.01 + 0.33 = 10.34 可归因；账户规模 21.00 − 10.34 = 10.66 无来源。
		require.Equal(t, 10.34, got.AttributedTotal)
		require.Equal(t, 10.66, got.Unattributed)
	})
}

func TestPrepareRefundQR(t *testing.T) {
	t.Parallel()

	t.Run("accepts a png", func(t *testing.T) {
		prepared, err := prepareRefundQR(encodeTestPNG(t, 64, 64))
		require.NoError(t, err)
		require.Equal(t, "image/png", prepared.contentType)
		require.True(t, strings.HasPrefix(prepared.key, "refund/"))
		require.True(t, strings.HasSuffix(prepared.key, ".png"))
	})

	t.Run("rejects oversized image", func(t *testing.T) {
		_, err := prepareRefundQR(encodeTestPNG(t, 2048, 2048))
		require.ErrorIs(t, err, ErrRefundQRInvalid)
	})

	t.Run("rejects non-image content", func(t *testing.T) {
		_, err := prepareRefundQR([]byte("<?xml version=\"1.0\"?><svg onload=\"alert(1)\"></svg>"))
		require.ErrorIs(t, err, ErrRefundQRInvalid)
	})

	t.Run("rejects declared jpeg that is not decodable", func(t *testing.T) {
		data := append([]byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}, bytesN(64)...) // JPEG 头 + 不完整数据
		_, err := prepareRefundQR(data)
		require.ErrorIs(t, err, ErrRefundQRInvalid)
	})

	t.Run("rejects empty and oversized payload", func(t *testing.T) {
		_, err := prepareRefundQR(nil)
		require.ErrorIs(t, err, ErrRefundQRInvalid)
		_, err = prepareRefundQR(bytesN(RefundQRMaxBytes + 1))
		require.ErrorIs(t, err, ErrRefundQRInvalid)
	})
}

func TestDecodeRefundQRImage(t *testing.T) {
	t.Parallel()

	raw := encodeTestPNG(t, 32, 32)
	encoded := base64.StdEncoding.EncodeToString(raw)

	// 前端 ImageUpload 给出 data URL，同时兼容裸 base64 与 URL-safe 变体。
	for _, payload := range []string{
		"data:image/png;base64," + encoded,
		encoded,
		"data:application/octet-stream;base64," + encoded,
		" " + encoded + "\n",
		strings.NewReplacer("+", "-", "/", "_").Replace(encoded),
	} {
		data, err := DecodeRefundQRImage(payload)
		require.NoError(t, err)
		require.Equal(t, raw, data)
	}

	_, err := DecodeRefundQRImage("   ")
	require.ErrorIs(t, err, ErrRefundQRRequired)

	_, err = DecodeRefundQRImage("data:image/png;base64")
	require.ErrorIs(t, err, ErrRefundQRInvalid)

	_, err = DecodeRefundQRImage("not-base64!!!")
	require.ErrorIs(t, err, ErrRefundQRInvalid)

	// 超长载荷直接拒，避免为了报错而先分配几 MB。
	_, err = DecodeRefundQRImage(strings.Repeat("A", base64EncodedMaxLen+1))
	require.ErrorIs(t, err, ErrRefundQRInvalid)
}

// TestRefundServiceWithoutClientFailsClosed 保证依赖缺失时不会静默放行：
// 冻结与结清都必须显式失败，而不是返回一个"成功"的空事务。
func TestRefundServiceWithoutClientFailsClosed(t *testing.T) {
	t.Parallel()

	svc := &RefundService{}
	err := svc.withTx(context.Background(), func(context.Context, *dbent.Client) error { return nil })
	require.Error(t, err)
	require.Contains(t, err.Error(), "REFUND_SERVICE_UNAVAILABLE")
}

// TestRefundFreezeHelpersIgnoreZeroAmount 冻结额为 0 时不产生任何 SQL 副作用，
// 也不会把用户的 frozen_balance 拖进负数。
func TestRefundFreezeHelpersIgnoreZeroAmount(t *testing.T) {
	t.Parallel()

	require.NoError(t, unfreezeUserBalance(context.Background(), nil, 1, 0))
	require.NoError(t, settleFrozenBalance(context.Background(), nil, 1, 0))
}

// TestAdminCompleteValidatesNoteBeforeTouchingDB 锁住完成备注的两条口径：
// 超长要在碰数据库之前就被拒；空备注是合法输入（可选字段）。
func TestAdminCompleteValidatesNoteBeforeTouchingDB(t *testing.T) {
	t.Parallel()

	svc := &RefundService{}

	long := strings.Repeat("a", RefundAdminNoteMaxLen+1)
	_, err := svc.AdminComplete(context.Background(), 1, 1, long)
	require.ErrorIs(t, err, ErrRefundAdminNoteTooLong)

	// 长度刚好合规，失败原因应该是服务未就绪而不是参数校验。
	_, err = svc.AdminComplete(context.Background(), 1, 1, strings.Repeat("a", RefundAdminNoteMaxLen))
	require.NotErrorIs(t, err, ErrRefundAdminNoteTooLong)

	// 空备注与纯空白备注都合法（可选字段），且不会因缺 client 之外的原因报错。
	for _, note := range []string{"", "   ", "支付宝已转，流水号 20261023"} {
		_, err = svc.AdminComplete(context.Background(), 1, 1, note)
		require.NotErrorIs(t, err, ErrRefundAdminNoteTooLong)
		require.NotErrorIs(t, err, ErrRefundRequestNotFound)
	}
}

func TestIsValidRefundStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []string{BalanceRefundStatusPending, BalanceRefundStatusCompleted, BalanceRefundStatusRejected} {
		require.True(t, isValidRefundStatus(status), status)
	}
	require.False(t, isValidRefundStatus("refunded"))
	require.False(t, isValidRefundStatus(""))
}

func encodeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func bytesN(n int) []byte {
	return make([]byte, n)
}
