export default {
  refund: {
    entry: '申请退款',
    dialogTitle: '申请余额退款',
    balance: '账户余额',
    rebate: '推广返利（已到账）',
    fee: '手续费（{rate}）',
    feeShort: '手续费',
    refundable: '可退款金额',
    rebateHint: '推广返利部分只能用于消费，不支持退款；手续费按扣除返利后的余额收取。',
    pendingTitle: '已有待处理的退款申请',
    pendingDesc: '{amount} · 提交于 {date}，请等待管理员处理。',
    qrLabel: '收款二维码',
    qrUpload: '选择图片',
    qrRemove: '移除',
    qrHint: '支持 JPEG / PNG / WebP，1MB 以内。管理员会按这张码向你打款。',
    noteLabel: '备注（选填）',
    notePlaceholder: '例如：支付宝收款，姓名 XXX',
    submit: '提交申请',
    submitted: '退款申请已提交，待退金额 {amount}',
    submitFailed: '提交退款申请失败',
    loadFailed: '获取可退款金额失败',
    frozenHint: '本次申请已冻结 {amount}，审核期间这部分余额不可消费；若被拒绝会原路退回。',
    frozenShort: '冻结 {amount}',
    belowMinimum: '可退款金额低于 {amount} 暂不支持退款——走一次人工打款不划算。继续消费或充值到门槛以上再来申请。',
    recordsTitle: '我的退款记录',
    recordsEmpty: '还没有提交过退款申请',
    rejectReason: '拒绝原因：',
    adminNote: '备注：',
    status: {
      pending: '审核中',
      completed: '已退款',
      rejected: '已拒绝'
    }
  }
}
