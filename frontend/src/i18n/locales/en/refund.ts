export default {
  refund: {
    entry: 'Request refund',
    dialogTitle: 'Request a balance refund',
    balance: 'Account balance',
    rebate: 'Affiliate rebate (credited)',
    fee: 'Handling fee ({rate})',
    feeShort: 'Handling fee',
    refundable: 'Refundable amount',
    rebateHint: 'Affiliate rebate can only be spent, not refunded. The handling fee is charged on the balance after the rebate is deducted.',
    pendingTitle: 'A refund request is already pending',
    pendingDesc: '{amount} · submitted {date}. Please wait for an administrator to review it.',
    qrLabel: 'Payment QR code',
    qrUpload: 'Choose image',
    qrRemove: 'Remove',
    qrHint: 'JPEG / PNG / WebP, up to 1MB. The administrator pays you using this code.',
    noteLabel: 'Note (optional)',
    notePlaceholder: 'e.g. Alipay, name XXX',
    submit: 'Submit request',
    submitted: 'Refund request submitted: {amount}',
    submitFailed: 'Failed to submit the refund request',
    loadFailed: 'Failed to load the refundable amount',
    frozenHint: '{amount} is frozen for this request and cannot be spent while it is under review; it is returned if the request is rejected.',
    frozenShort: 'frozen {amount}',
    belowMinimum: 'Refunds below {amount} are not supported — a manual payout is not worth the change. Keep spending it or top up above the threshold.',
    recordsTitle: 'My refund requests',
    recordsEmpty: 'No refund requests yet',
    rejectReason: 'Rejection reason: ',
    adminNote: 'Note: ',
    status: {
      pending: 'Under review',
      completed: 'Refunded',
      rejected: 'Rejected'
    }
  }
}
