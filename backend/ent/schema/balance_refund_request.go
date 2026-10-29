package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BalanceRefundRequest 记录一笔用户发起的余额退款申请。
//
// 金额四件套（balance / rebate_amount / fee_amount / refundable_amount）都是提交
// 时刻的快照：费率或返利之后发生变化都不会改写历史，管理员审核时看到的就是
// 用户当时确认的那笔钱。
type BalanceRefundRequest struct {
	ent.Schema
}

func (BalanceRefundRequest) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "balance_refund_requests"},
	}
}

func (BalanceRefundRequest) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (BalanceRefundRequest) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		// 申请时的账户余额（原精度 8 位小数，展示口径由服务层收敛到 2 位）。
		field.Float("balance").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		// 已到账返利：累计通过「返利转余额」进入余额的金额，不参与退款。
		field.Float("rebate_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		// 手续费比例快照（百分比，0-100）。
		field.Float("fee_rate_percent").
			SchemaType(map[string]string{dialect.Postgres: "decimal(5,2)"}).
			Default(0),
		field.Float("fee_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Default(0),
		// 可退款金额 =（余额 − 已到账返利）×(1 − 费率)，保留两位小数。
		field.Float("refundable_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Default(0),
		// 提交时从 users.balance 转入 users.frozen_balance 的金额（整笔余额）。
		// 审核期间这笔钱不可消费：拒绝则解冻退回余额，完成则就地结清。
		field.Float("frozen_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
		// 收款二维码图片在 SupportAttachmentStore 中的 key，不对外公开。
		field.String("qr_storage_key").
			MaxLen(500).
			NotEmpty(),
		field.String("qr_content_type").
			MaxLen(64).
			Default("image/png"),
		field.Int64("qr_size").
			Default(0),
		field.String("status").
			MaxLen(20).
			Default("pending").
			NotEmpty(),
		field.String("user_note").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		// 管理员拒绝时必填的原因；完成时可为空。
		field.String("reject_reason").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		// 管理员完成退款时可选填写的备注（打款流水号、渠道单号等）。
		// 与拒绝原因一样对用户可见，所以管理员写之前知道这是"给对方看的"。
		field.String("admin_note").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Int64("reviewed_by").
			Optional().
			Nillable(),
		field.Time("reviewed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (BalanceRefundRequest) Indexes() []ent.Index {
	return []ent.Index{
		// 一个用户最多只能有一笔待处理申请，由服务层校验 + 该部分索引兜底。
		index.Fields("user_id").
			StorageKey("balance_refund_requests_pending_uq").
			Annotations(entsql.IndexWhere("status = 'pending'")).
			Unique(),
		index.Fields("status", "created_at"),
		index.Fields("user_id", "created_at"),
	}
}
