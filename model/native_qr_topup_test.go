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

// RefundNativeQR（与 chatgpt 项目一致的同步退款结算）必须保证：按到账额度快照精确扣回一次、
// 重复退款幂等、保留原支付完成时间、渠道不匹配拒绝、非成功订单拒绝、无快照的历史订单拒绝。

// createNativeSuccessOrderWithSnapshot 建一笔已成功、带到账额度快照的原生扫码订单。
// completeTime 为原支付完成时间，用于校验退款不覆盖它。
func createNativeSuccessOrderWithSnapshot(t *testing.T, userId int, tradeNo, provider string, creditedQuota int, completeTime int64) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   provider,
		PaymentProvider: provider,
		CreateTime:      completeTime,
		CompleteTime:    completeTime,
		Status:          common.TopUpStatusSuccess,
		CreditedQuota:   creditedQuota,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func TestRefundNativeQRDeductsSnapshotExactlyOnce(t *testing.T) {
	truncateTables(t)

	// 用户余额含已充值的快照 1,000,000 加上额外结余 300000
	user := insertUserForPaymentGuardTest(t, 611, 1_000_000+300_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDONCE", PaymentProviderWechatNative, 1_000_000, 1000)

	alreadyDone, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 300_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusRefunded, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))

	// 重复退款必须幂等，不再二次扣额
	alreadyDone, err = RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 300_000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

// 退款按结算时的到账额度快照原数扣回，而不是用当前 QuotaPerUnit 重算——
// 否则结算后费率变化会导致扣回额度错误。
func TestRefundNativeQRUsesCreditedQuotaSnapshot(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	// 结算后把费率改小，若按 Amount×当前费率重算只会扣回 2*250000=500000（错误）
	common.QuotaPerUnit = 250000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 621, 1_000_000+300_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDSNAPSHOT", PaymentProviderWechatNative, 1_000_000, 1000)

	alreadyDone, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	// 必须按快照 1,000,000 扣回，剩余 300000
	assert.Equal(t, 300_000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

// 退款不得覆盖原支付完成时间 CompleteTime，退款时间写入独立字段 RefundTime。
func TestRefundNativeQRPreservesCompleteTimeAndSetsRefundTime(t *testing.T) {
	truncateTables(t)

	const paidAt int64 = 1_700_000_000
	user := insertUserForPaymentGuardTest(t, 624, 1_000_000)
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDTIME", PaymentProviderWechatNative, 1_000_000, paidAt)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.NoError(t, err)

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, paidAt, reloaded.CompleteTime, "原支付完成时间必须保留")
	assert.GreaterOrEqual(t, reloaded.RefundTime, paidAt, "退款时间写入独立字段")
}

func TestRefundNativeQRRejectsMismatchedProvider(t *testing.T) {
	truncateTables(t)

	user := insertUserForPaymentGuardTest(t, 612, 1_000_000)
	// 订单属于支付宝渠道，却用微信渠道来退款，必须拒绝且不扣额
	order := createNativeSuccessOrderWithSnapshot(t, user.Id, "NATIVEREFUNDMISMATCH", PaymentProviderAlipayNative, 1_000_000, 1000)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRefundNativeQRRejectsNonSuccessOrder(t *testing.T) {
	truncateTables(t)

	user := insertUserForPaymentGuardTest(t, 613, 1_000_000)
	order := createEpayTestOrder(t, user.Id, "NATIVEREFUNDPENDING", PaymentProviderWechatNative, common.TopUpStatusPending)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotRefundable)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

// 无到账额度快照的历史订单（CreditedQuota<=0）拒绝自动扣回，避免用当前费率错误扣费。
func TestRefundNativeQRRejectsOrderWithoutSnapshot(t *testing.T) {
	truncateTables(t)

	user := insertUserForPaymentGuardTest(t, 625, 1_000_000)
	// createEpayTestOrder 不设置 CreditedQuota（=0），模拟升级前的历史订单
	order := createEpayTestOrder(t, user.Id, "NATIVEREFUNDNOSNAP", PaymentProviderWechatNative, common.TopUpStatusSuccess)

	_, err := RefundNativeQR(order.TradeNo, PaymentProviderWechatNative, "127.0.0.1")
	require.ErrorIs(t, err, ErrRefundNoQuotaSnapshot)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}
