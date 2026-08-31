package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func GetTopUpInfo(c *gin.Context) {
	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()

	// 获取支付方式：易支付聚合的方式（alipay/wxpay/custom 等）都需要易支付凭据，
	// 仅在易支付真正可用时展示，否则用户会点到不可用的方式（如仅配置了原生扫码时）。
	payMethods := []map[string]string{}
	if isEpayTopUpEnabled() {
		payMethods = operation_setting.PayMethods
	}

	// 如果启用了 Stripe 支付，添加到支付方法列表
	if isStripeTopUpEnabled() {
		// 检查是否已经包含 Stripe
		hasStripe := false
		for _, method := range payMethods {
			if method["type"] == "stripe" {
				hasStripe = true
				break
			}
		}

		if !hasStripe {
			stripeMethod := map[string]string{
				"name":      "Stripe",
				"type":      "stripe",
				"color":     "#635BFF",
				"min_topup": strconv.Itoa(setting.StripeMinTopUp),
			}
			payMethods = append(payMethods, stripeMethod)
		}
	}

	// Waffo Pancake is displayed above the standard Waffo gateway.
	enableWaffoPancake := isWaffoPancakeTopUpEnabled()
	if enableWaffoPancake {
		hasWaffoPancake := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffoPancake {
				hasWaffoPancake = true
				break
			}
		}

		if !hasWaffoPancake {
			payMethods = append(payMethods, map[string]string{
				"name":      "Waffo Pancake",
				"type":      model.PaymentMethodWaffoPancake,
				"color":     "#F97316",
				"min_topup": strconv.Itoa(setting.WaffoPancakeMinTopUp),
			})
		}
	}

	// 如果启用了 Waffo 支付，添加到支付方法列表
	enableWaffo := isWaffoTopUpEnabled()
	if enableWaffo {
		hasWaffo := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffo {
				hasWaffo = true
				break
			}
		}

		if !hasWaffo {
			waffoMethod := map[string]string{
				"name":      "Waffo (Global Payment)",
				"type":      model.PaymentMethodWaffo,
				"color":     "#3B82F6",
				"min_topup": strconv.Itoa(setting.WaffoMinTopUp),
			}
			payMethods = append(payMethods, waffoMethod)
		}
	}

	// 微信/支付宝原生扫码支付（区别于易支付聚合的 alipay/wxpay，type 独立、走二维码流程）
	enableWechatNative := isWechatNativeTopUpEnabled()
	if enableWechatNative {
		payMethods = append(payMethods, map[string]string{
			"name":  "微信支付",
			"type":  model.PaymentMethodWechatNative,
			"color": "#07C160",
			"flow":  "qr",
		})
	}
	enableAlipayNative := isAlipayNativeTopUpEnabled()
	if enableAlipayNative {
		payMethods = append(payMethods, map[string]string{
			"name":  "支付宝",
			"type":  model.PaymentMethodAlipayNative,
			"color": "#1677FF",
			"flow":  "qr",
		})
	}

	data := gin.H{
		"enable_online_topup":              isEpayTopUpEnabled(),
		"enable_stripe_topup":              isStripeTopUpEnabled(),
		"enable_creem_topup":               isCreemTopUpEnabled(),
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_wechat_native_topup":       enableWechatNative,
		"enable_alipay_native_topup":       enableAlipayNative,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() interface{} {
			if enableWaffo {
				return setting.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          setting.CreemProducts,
		"pay_methods":             payMethods,
		"min_topup":               operation_setting.MinTopUp,
		"stripe_min_topup":        setting.StripeMinTopUp,
		"waffo_min_topup":         setting.WaffoMinTopUp,
		"waffo_pancake_min_topup": setting.WaffoPancakeMinTopUp,
		"amount_options":          operation_setting.GetPaymentSetting().AmountOptions,
		"discount":                operation_setting.GetPaymentSetting().AmountDiscount,
		"topup_link":              common.TopUpLink,
	}
	common.ApiSuccess(c, data)
}

type EpayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

func GetEpayClient() *epay.Client {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil
	}
	withUrl, err := epay.NewClient(&epay.Config{
		PartnerID: operation_setting.EpayId,
		Key:       operation_setting.EpayKey,
	}, operation_setting.PayAddress)
	if err != nil {
		return nil
	}
	return withUrl
}

func getPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	// 充值金额以“展示类型”为准：
	// - USD/CNY: 前端传 amount 为金额单位；TOKENS: 前端传 tokens，需要换成 USD 金额
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		dAmount = dAmount.Div(dQuotaPerUnit)
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)
	dPrice := decimal.NewFromFloat(operation_setting.Price)
	// apply optional preset discount by the original request amount (if configured), default 1.0
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)

	return payMoney.InexactFloat64()
}

func getMinTopup() int64 {
	minTopup := operation_setting.MinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quota, err := common.WalletQuotaFromDecimalStrict(dMinTopup.Mul(dQuotaPerUnit))
		if err != nil {
			return common.MaxWalletQuota
		}
		minTopup = quota
	}
	return int64(minTopup)
}

func getTopUpQuota(amount int64) (int, error) {
	quota := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quota = decimal.NewFromInt(quota.Div(quotaPerUnit).IntPart()).Mul(quotaPerUnit)
	} else {
		quota = quota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return common.WalletQuotaFromDecimalStrict(quota)
}

func getMaxTopUpAmount() int64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	maxStoredAmount := decimal.NewFromInt(common.MaxWalletQuota).
		Div(quotaPerUnit).
		Floor()
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return maxStoredAmount.Add(decimal.NewFromInt(1)).
			Mul(quotaPerUnit).
			Ceil().
			Sub(decimal.NewFromInt(1)).
			IntPart()
	}
	return maxStoredAmount.IntPart()
}

func validateCreditedQuota(quota decimal.Decimal) (int, error) {
	value, err := common.WalletQuotaFromDecimalStrict(quota)
	if err != nil {
		return 0, errors.New("充值额度超出系统可表示范围")
	}
	if value <= 0 {
		return 0, errors.New("充值额度必须大于 0")
	}
	return value, nil
}

func validateTopUpQuota(amount int64) (int, error) {
	quota, err := getTopUpQuota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maxAmount := getMaxTopUpAmount()
	if maxAmount > 0 && amount > maxAmount {
		return 0, fmt.Errorf("单笔充值数量不能大于 %d", maxAmount)
	}
	return 0, errors.New("充值数量无效")
}

func rejectInvalidCreditedQuota(c *gin.Context, userId int, quota decimal.Decimal) bool {
	creditedQuota, err := validateCreditedQuota(quota)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func rejectInvalidTopUpQuota(c *gin.Context, userId int, amount int64) bool {
	creditedQuota, err := validateTopUpQuota(amount)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": err.Error()})
		return true
	}
	return false
}

func RequestEpay(c *gin.Context) {
	var req EpayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}

	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "支付方式不存在"})
		return
	}

	callBackAddress := service.GetCallbackAddress()
	returnUrl, _ := url.Parse(paymentReturnPath("/usage-logs"))
	notifyUrl, _ := url.Parse(callBackAddress + "/api/user/epay/notify")
	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	tradeNo = fmt.Sprintf("USR%dNO%s", id, tradeNo)
	client := GetEpayClient()
	if client == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "当前管理员未配置支付信息"})
		return
	}
	uri, params, err := client.Purchase(&epay.PurchaseArgs{
		Type:           req.PaymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", req.Amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyUrl,
		ReturnUrl:      returnUrl,
	})
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 拉起支付失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dAmount := decimal.NewFromInt(int64(amount))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		amount = dAmount.Div(dQuotaPerUnit).IntPart()
	}
	topUp := &model.TopUp{
		UserId:          id,
		Amount:          amount,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   req.PaymentMethod,
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	err = topUp.Insert()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 创建充值订单失败 user_id=%d trade_no=%s payment_method=%s amount=%d error=%q", id, tradeNo, req.PaymentMethod, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f uri=%q params=%q", id, tradeNo, req.PaymentMethod, req.Amount, payMoney, uri, common.GetJsonString(params)))
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": params, "url": uri})
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// refCountedMutex 带引用计数的互斥锁，确保最后一个使用者才从 map 中删除
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	v, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func EpayNotify(c *gin.Context) {
	if !isEpayWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}

	var params map[string]string

	if c.Request.Method == "POST" {
		// POST 请求：从 POST body 解析参数
		if err := c.Request.ParseForm(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		params = lo.Reduce(lo.Keys(c.Request.PostForm), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.PostForm.Get(t)
			return r
		}, map[string]string{})
	} else {
		// GET 请求：从 URL Query 解析参数
		params = lo.Reduce(lo.Keys(c.Request.URL.Query()), func(r map[string]string, t string, i int) map[string]string {
			r[t] = c.Request.URL.Query().Get(t)
			return r
		}, map[string]string{})
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 收到请求 path=%q client_ip=%s method=%s params=%q", c.Request.RequestURI, c.ClientIP(), c.Request.Method, common.GetJsonString(params)))

	if len(params) == 0 {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 参数为空 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	client := GetEpayClient()
	if client == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 client 未初始化 path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP()))
		_, err := c.Writer.Write([]byte("fail"))
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		}
		return
	}
	verifyInfo, err := client.Verify(params)
	if err != nil || !verifyInfo.VerifyStatus {
		if _, writeErr := c.Writer.Write([]byte("fail")); writeErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), writeErr.Error()))
		}
		if err != nil {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_error=%q", c.Request.RequestURI, c.ClientIP(), err.Error()))
		} else {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.Request.RequestURI, c.ClientIP()))
		}
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 验签成功 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP(), common.GetJsonString(verifyInfo)))

	if verifyInfo.TradeStatus == epay.StatusTradeSuccess {
		// 进程内锁只是优化；重复/并发回调的正确性由 RechargeEpay 的
		// 数据库行锁 + 事务内状态校验保证（多实例部署下同样安全）。
		LockOrder(verifyInfo.ServiceTradeNo)
		defer UnlockOrder(verifyInfo.ServiceTradeNo)
		alreadyDone, err := model.RechargeEpay(verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP())
		if err != nil {
			switch {
			case errors.Is(err, model.ErrTopUpNotFound):
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 回调订单不存在 trade_no=%s callback_type=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP(), common.GetJsonString(verifyInfo)))
			case errors.Is(err, model.ErrPaymentMethodMismatch):
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 订单支付网关不匹配 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
			case errors.Is(err, model.ErrTopUpStatusInvalid):
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("易支付 订单状态非法 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
			default:
				logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 充值处理失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), err.Error()))
			}
			if _, writeErr := c.Writer.Write([]byte("fail")); writeErr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
			}
			return
		}
		if alreadyDone {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 重复回调幂等忽略 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		} else {
			logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 充值成功 trade_no=%s callback_type=%s client_ip=%s", verifyInfo.ServiceTradeNo, verifyInfo.Type, c.ClientIP()))
		}
	} else {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("易支付 webhook 忽略事件 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", verifyInfo.ServiceTradeNo, verifyInfo.Type, verifyInfo.TradeStatus, c.ClientIP(), common.GetJsonString(verifyInfo)))
	}
	if _, writeErr := c.Writer.Write([]byte("success")); writeErr != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", verifyInfo.ServiceTradeNo, c.ClientIP(), writeErr.Error()))
	}
}

func RequestAmount(c *gin.Context) {
	var req AmountRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.Amount < getMinTopup() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	id := c.GetInt("id")
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "获取用户分组失败"})
		return
	}
	payMoney := getPayMoney(req.Amount, group)
	if payMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(payMoney, 'f', 2, 64)})
}

func GetUserTopUps(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = model.GetUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = model.GetAllTopUps(pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

// AdminCompleteTopUp 管理员补单接口
func AdminCompleteTopUp(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	// 订单级互斥，防止并发补单
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	if err := model.ManualCompleteTopUp(req.TradeNo, c.ClientIP()); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

type AdminRefundTopupRequest struct {
	TradeNo string `json:"trade_no"`
	Reason  string `json:"reason"`
}

// isNativeQROrder 判断订单是否为微信/支付宝原生扫码渠道。
func isNativeQROrder(topUp *model.TopUp) bool {
	return topUp.PaymentProvider == model.PaymentProviderWechatNative ||
		topUp.PaymentProvider == model.PaymentProviderAlipayNative
}

// 后台退款对账任务参数。
const (
	nativeReconcileInterval     = 2 * time.Minute
	nativeReconcileBatch        = 100
	nativeReconcileQueryTimeout = 15 * time.Second
)

// applyRefundOutcome 依据渠道退款结果推进本地订单状态，返回结果状态。
// success→回滚额度并置 refunded；closed→回退 success；abnormal→refund_failed；
// processing→保持 refund_pending（交由后台对账继续确认）。供退款接口与对账任务复用。
func applyRefundOutcome(tradeNo, provider, callerIp string, outcome service.RefundOutcome) (string, error) {
	switch outcome {
	case service.RefundOutcomeSuccess:
		if _, err := model.RefundNativeQR(tradeNo, provider, callerIp); err != nil {
			return "", err
		}
		return common.TopUpStatusRefunded, nil
	case service.RefundOutcomeClosed:
		if err := model.RevertRefundPendingToSuccess(tradeNo, provider); err != nil {
			return "", err
		}
		return common.TopUpStatusSuccess, nil
	case service.RefundOutcomeAbnormal:
		if err := model.MarkRefundFailedNativeQR(tradeNo, provider); err != nil {
			return "", err
		}
		return common.TopUpStatusRefundFailed, nil
	default: // RefundOutcomeProcessing
		return common.TopUpStatusRefundPending, nil
	}
}

// AdminRefundTopUp 管理员对微信/支付宝原生扫码订单发起全额退款。
// 健壮流程：发起前先持久化 refund_pending，再向渠道申请退款（out_refund_no 幂等）；
// 请求内做短暂快速确认（UX），未确认的订单保持 refund_pending，由后台对账任务
// (ReconcileNativeRefunds) 脱离本请求继续确认结算——因此服务重启/请求中断不会丢单。
func AdminRefundTopUp(c *gin.Context) {
	var req AdminRefundTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	// 订单级互斥，防止并发退款
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	topUp := model.GetTopUpByTradeNo(req.TradeNo)
	if topUp == nil {
		common.ApiErrorMsg(c, "订单不存在")
		return
	}
	if !isNativeQROrder(topUp) {
		common.ApiErrorMsg(c, "仅支持微信/支付宝原生扫码订单退款")
		return
	}
	// 幂等：已退款直接返回成功（首次成功但响应丢失时，重试不再报错）
	if topUp.Status == common.TopUpStatusRefunded {
		common.ApiSuccess(c, gin.H{"status": common.TopUpStatusRefunded})
		return
	}
	if topUp.Status == common.TopUpStatusRefundFailed {
		common.ApiErrorMsg(c, "退款异常，请到商户平台核实处理")
		return
	}
	// 允许 success（首次退款）与 refund_pending（重试/手动催单）发起
	if topUp.Status != common.TopUpStatusSuccess && topUp.Status != common.TopUpStatusRefundPending {
		common.ApiErrorMsg(c, "仅支付成功的订单可退款")
		return
	}
	// 缺少到账额度快照的历史订单拒绝自动退款，避免用当前费率错误扣回
	if topUp.CreditedQuota <= 0 {
		common.ApiErrorMsg(c, "该订单缺少到账额度快照，无法自动退款，请人工核对处理")
		return
	}

	reason := req.Reason
	if reason == "" {
		reason = "管理员退款"
	}

	// 发起前先持久化 refund_pending，确保即使后续中断/重启，后台对账仍能接手
	if err := model.MarkRefundPendingNativeQR(topUp.TradeNo, topUp.PaymentProvider); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("原生扫码 标记退款处理中失败 trade_no=%s provider=%s error=%q", topUp.TradeNo, topUp.PaymentProvider, err.Error()))
		common.ApiErrorMsg(c, "发起退款失败："+err.Error())
		return
	}

	// 向渠道发起退款并做短暂快速确认；金额以本地订单为准防篡改。
	// 申请不确定时内部会按 out_refund_no 查询确认；error 仅表示发起前的配置/参数问题。
	outcome, err := service.NativeRefundSubmit(c.Request.Context(), topUp.PaymentProvider, topUp.TradeNo, reason, topUp.Money, topUp.Money, true)
	if err != nil {
		// 尚未真正发起，回退处理中状态，避免订单空挂
		_ = model.RevertRefundPendingToSuccess(topUp.TradeNo, topUp.PaymentProvider)
		logger.LogError(c.Request.Context(), fmt.Sprintf("原生扫码 退款发起失败 trade_no=%s provider=%s error=%q", topUp.TradeNo, topUp.PaymentProvider, err.Error()))
		common.ApiErrorMsg(c, "退款失败："+err.Error())
		return
	}

	status, err := applyRefundOutcome(topUp.TradeNo, topUp.PaymentProvider, c.ClientIP(), outcome)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("原生扫码 退款结算失败 trade_no=%s provider=%s outcome=%d error=%q", topUp.TradeNo, topUp.PaymentProvider, outcome, err.Error()))
		common.ApiErrorMsg(c, "渠道退款已受理，但本地结算失败，请稍后在订单管理查看："+err.Error())
		return
	}

	switch status {
	case common.TopUpStatusRefunded, common.TopUpStatusRefundPending:
		// refunded：已完成；refund_pending：渠道处理中，后台对账将自动确认到账
		common.ApiSuccess(c, gin.H{"status": status})
	case common.TopUpStatusRefundFailed:
		common.ApiErrorMsg(c, "退款异常，请到商户平台核实处理")
	default: // 回退为 success，即渠道退款已关闭、未发生扣款
		common.ApiErrorMsg(c, "退款已关闭，未发生扣款")
	}
}

// ReconcileNativeRefunds 扫描退款处理中（refund_pending）的原生扫码订单，以幂等退款单号
// 重新提交/确认并推进结算。脱离用户请求运行，保证渠道最终退款成功后本地额度一定被回滚
// （重启/断连/首次申请从未真正发出都不丢单）。异常退款（refund_failed）为人工终态，不在此自动处理。
// 用 id 游标翻页遍历全量，避免最早 N 条长时间处理中时饿死后续退款。
func ReconcileNativeRefunds() {
	afterId := 0
	for {
		orders, err := model.GetReconcilableNativeRefunds(afterId, nativeReconcileBatch)
		if err != nil {
			common.SysError("原生扫码 退款对账拉取订单失败: " + err.Error())
			return
		}
		for _, order := range orders {
			reconcileOneNativeRefund(order.TradeNo, order.PaymentProvider, order.Money)
			if order.Id > afterId {
				afterId = order.Id
			}
		}
		if len(orders) < nativeReconcileBatch {
			return
		}
	}
}

func reconcileOneNativeRefund(tradeNo, provider string, refundYuan float64) {
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	// 加锁后重读，避免与退款接口/其它对账并发
	cur := model.GetTopUpByTradeNo(tradeNo)
	if cur == nil || cur.Status != common.TopUpStatusRefundPending {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), nativeReconcileQueryTimeout)
	defer cancel()
	// 以幂等退款单号（out_refund_no=trade_no）重新提交/确认：首次申请若因中断/重启从未真正发出，
	// 在此补发；已受理的退款则返回既有退款单当前状态。金额以本地订单为准，fastConfirm=false 不在对账内长轮询。
	outcome, err := service.NativeRefundSubmit(ctx, provider, tradeNo, "管理员退款", refundYuan, refundYuan, false)
	if err != nil {
		// 发起前的配置/参数问题（非渠道结果）：保持当前状态，下一轮再试
		logger.LogWarn(ctx, fmt.Sprintf("原生扫码 退款对账提交失败 trade_no=%s provider=%s error=%q", tradeNo, provider, err.Error()))
		return
	}
	if _, err := applyRefundOutcome(tradeNo, provider, "reconcile", outcome); err != nil {
		common.SysError(fmt.Sprintf("原生扫码 退款对账结算失败 trade_no=%s provider=%s outcome=%d error=%s", tradeNo, provider, outcome, err.Error()))
	}
}

var (
	nativeReconcileOnce          sync.Once
	nativeTopupReconcileRunning  atomic.Bool
	nativeRefundReconcileRunning atomic.Bool
)

// StartNativeOrderReconciler 周期性对账微信/支付宝原生扫码订单，直到进程退出：
//   - 待支付充值订单：主动查渠道，已支付则结算到账、超时未支付则置为过期
//   - 退款处理中订单：以幂等退款单号重新提交/确认，成功则回滚额度
//
// 保证服务重启/断连/漏回调时订单仍被自动补偿。充值与退款各自独立 goroutine + 独立节流，
// 退款对账不再排在充值全量扫描之后被阻塞。仅主节点运行（与订阅重置、鉴权清理等维护任务一致），
// 避免多节点重复查询渠道；正确性另由订单行锁与幂等结算保证。非阻塞，由 main 直接调用。
func StartNativeOrderReconciler() {
	nativeReconcileOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			runNativeReconcileLoop("充值", &nativeTopupReconcileRunning, ReconcilePendingNativeTopups)
		})
		gopool.Go(func() {
			runNativeReconcileLoop("退款", &nativeRefundReconcileRunning, ReconcileNativeRefunds)
		})
	})
}

// runNativeReconcileLoop 以固定间隔运行一个自节流的对账循环：上一轮未结束则跳过本轮，
// 避免订单量大、渠道超时导致的多轮叠加。供充值/退款两个对账任务复用。
func runNativeReconcileLoop(name string, running *atomic.Bool, run func()) {
	ticker := time.NewTicker(nativeReconcileInterval)
	defer ticker.Stop()
	guarded := func() {
		if !running.CompareAndSwap(false, true) {
			logger.LogWarn(context.Background(), "原生扫码 "+name+"对账上一轮仍在进行，跳过本轮")
			return
		}
		defer running.Store(false)
		run()
	}
	guarded()
	for range ticker.C {
		guarded()
	}
}
