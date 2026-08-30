package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RechargeNativeQR 结算被前端轮询和异步回调共用，必须保证：同一订单最多充值一次、
// 渠道不匹配时拒绝、金额溢出时在完成订单前失败且不扣不加。以下用例直接守护这些不变量。

func TestRechargeNativeQRCreditsQuotaExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 601, 0)
	order := createEpayTestOrder(t, user.Id, "NATIVEONCE", PaymentProviderWechatNative, common.TopUpStatusPending)

	alreadyDone, err := RechargeNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	// 重复调用（回调 + 轮询同时到达）必须幂等，不再二次充值
	alreadyDone, err = RechargeNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeNativeQRRejectsMismatchedProvider(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 602, 7)
	// 订单实际属于支付宝渠道，却用微信渠道来结算，必须拒绝
	order := createEpayTestOrder(t, user.Id, "NATIVEMISMATCH", PaymentProviderAlipayNative, common.TopUpStatusPending)

	_, err := RechargeNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRechargeNativeQRRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = float64(common.MaxWalletQuota + 1)
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 603, 3)
	order := createEpayTestOrder(t, user.Id, "NATIVEOVERFLOW", PaymentProviderAlipayNative, common.TopUpStatusPending)

	_, err := RechargeNativeQR(order.TradeNo, PaymentProviderAlipayNative, "127.0.0.1")
	require.Error(t, err)
	assert.Equal(t, 3, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

// RefundNativeQR 在渠道退款成功后回滚本地记账，必须保证：已充值额度被精确扣回一次、
// 重复退款幂等、渠道不匹配拒绝、非成功订单不可退款。以下用例直接守护这些不变量。

func TestRefundNativeQRDeductsQuotaExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	// 用户余额含已充值的 2 单位（2*500000）加上额外结余 300000
	user := insertUserForPaymentGuardTest(t, 611, 2*500000+300000)
	order := createEpayTestOrder(t, user.Id, "NATIVEREFUNDONCE", PaymentProviderWechatNative, common.TopUpStatusSuccess)

	alreadyDone, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 300000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusRefunded, reloaded.Status)

	// 重复退款必须幂等，不再二次扣额
	alreadyDone, err = RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 300000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRefundNativeQRRejectsMismatchedProvider(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 612, 1000000)
	// 订单属于支付宝渠道，却用微信渠道来退款，必须拒绝且不扣额
	order := createEpayTestOrder(t, user.Id, "NATIVEREFUNDMISMATCH", PaymentProviderAlipayNative, common.TopUpStatusSuccess)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.Equal(t, 1000000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRefundNativeQRRejectsNonSuccessOrder(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 613, 1000000)
	order := createEpayTestOrder(t, user.Id, "NATIVEREFUNDPENDING", PaymentProviderWechatNative, common.TopUpStatusPending)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotRefundable)
	assert.Equal(t, 1000000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

// createNativeSuccessOrderWithSnapshot 建一笔已成功、带到账额度快照的原生扫码订单。
func createNativeSuccessOrderWithSnapshot(t *testing.T, userId int, tradeNo, provider string, creditedQuota int) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   provider,
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusSuccess,
		CreditedQuota:   creditedQuota,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

// 退款必须按结算时的到账额度快照扣回，而不是用当前 QuotaPerUnit 重新计算——
// 否则结算后费率变化会导致扣回额度错误。
func TestRefundNativeQRUsesCreditedQuotaSnapshot(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	// 结算时费率 500000，到账 1,000,000（快照）
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 621, 1_000_000+300_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDSNAPSHOT", PaymentProviderWechatNative, 1_000_000)

	// 结算后把费率改小，若按 Amount×当前费率重算只会扣回 2*250000=500000（错误）
	common.QuotaPerUnit = 250000

	alreadyDone, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	// 必须按快照 1,000,000 扣回，剩余 300000
	assert.Equal(t, 300_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusRefunded, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

// 微信异步退款 PROCESSING 时只置为 refund_pending，不得扣额度；查询确认成功后再扣回快照。
func TestMarkRefundPendingDefersDeductionUntilFinalize(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 622, 1_000_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDPENDINGFLOW", PaymentProviderWechatNative, 1_000_000)

	// 处理中：不扣额度，状态置为 refund_pending
	require.NoError(t, MarkRefundPendingNativeQR(order.TradeNo, PaymentProviderWechatNative))
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusRefundPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))

	// 幂等：再次标记处理中不报错、不变更
	require.NoError(t, MarkRefundPendingNativeQR(order.TradeNo, PaymentProviderWechatNative))
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))

	// 查询确认成功后结算：从 refund_pending 扣回快照并置 refunded
	alreadyDone, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusRefunded, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

// 渠道退款最终失败时，把处理中的订单退回 success，且不涉及额度变动。
func TestRevertRefundPendingRestoresSuccess(t *testing.T) {
	truncateTables(t)

	user := insertUserForPaymentGuardTest(t, 623, 1_000_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDREVERT", PaymentProviderWechatNative, 1_000_000)

	require.NoError(t, MarkRefundPendingNativeQR(order.TradeNo, PaymentProviderWechatNative))
	assert.Equal(t, common.TopUpStatusRefundPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))

	require.NoError(t, RevertRefundPendingNativeQR(order.TradeNo, PaymentProviderWechatNative))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))
}
