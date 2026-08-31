package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/go-pay/gopay"
	"github.com/go-pay/gopay/alipay"
	wechat "github.com/go-pay/gopay/wechat/v3"
	"github.com/shopspring/decimal"
)

// native_pay.go 封装微信支付 V3 Native 与支付宝 trade.precreate 原生扫码支付。
// 下单返回支付串（微信 code_url / 支付宝 qr_code），由前端渲染成二维码。
// 支付状态既可由前端轮询 NativeQuery 主动查询，也可由异步回调 VerifyXxxNotify 验证；
// 两条路径最终都汇聚到 model.RechargeNativeQR 完成幂等结算。

const alipayTradeStatusSuccess = "TRADE_SUCCESS"
const alipayTradeStatusFinished = "TRADE_FINISHED"

// 微信 V3 退款单状态（申请退款/查询退款返回）
const (
	wechatRefundStatusSuccess    = "SUCCESS"    // 退款成功
	wechatRefundStatusClosed     = "CLOSED"     // 退款关闭
	wechatRefundStatusProcessing = "PROCESSING" // 退款处理中
	wechatRefundStatusAbnormal   = "ABNORMAL"   // 退款异常
)

// 支付宝退款查询状态：退款成功返回 REFUND_SUCCESS
const alipayRefundStatusSuccess = "REFUND_SUCCESS"

// NativeOrderValiditySeconds 原生扫码订单有效期（秒）。下单时写入渠道截止时间
// （微信 time_expire / 支付宝 timeout_express），使渠道在此后拒绝支付，与充值对账的
// 本地过期判定保持一致——杜绝“本地判过期、渠道仍可付款”导致的已付款不到账窗口。
const NativeOrderValiditySeconds int64 = 2 * 60 * 60

// 退款申请后请求内的快速确认轮询参数（仅用于常见即时到账的 UX 优化，约 10s）。
// 正确性不依赖此轮询：未在窗口内确认的订单落 refund_pending，由后台对账任务兜底。
const (
	nativeRefundPollInterval = 2 * time.Second
	nativeRefundFastPolls    = 5
)

// RefundOutcome 表示一次退款申请/查询归一化后的结果。
type RefundOutcome int

const (
	RefundOutcomeSuccess    RefundOutcome = iota // 已确认退款成功，可安全回滚本地额度
	RefundOutcomeProcessing                      // 受理中/结果不确定，需后台对账继续确认
	RefundOutcomeClosed                          // 退款已关闭（未出款），可回退订单为 success
	RefundOutcomeAbnormal                        // 退款异常，需人工到商户平台处理
)

// ---- 支付宝客户端（密钥模式，构造开销小，按需创建）----

func newAlipayClient() (*alipay.Client, error) {
	if setting.AlipayAppID == "" || setting.AlipayPrivateKey == "" {
		return nil, errors.New("支付宝支付未配置")
	}
	client, err := alipay.NewClient(setting.AlipayAppID, setting.AlipayPrivateKey, setting.AlipayIsProd)
	if err != nil {
		return nil, err
	}
	client.SetSignType(alipay.RSA2)
	// 配置支付宝公钥后自动校验同步响应签名
	if setting.AlipayPublicKey != "" {
		client.AutoVerifySign([]byte(setting.AlipayPublicKey))
	}
	return client, nil
}

// ---- 微信 V3 客户端（AutoVerifySign 会拉取并自动刷新平台证书，故缓存复用）----

var (
	wechatClientMu  sync.Mutex
	wechatClient    *wechat.ClientV3
	wechatClientSig string
)

func configFingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func getWechatClient() (*wechat.ClientV3, error) {
	if setting.WechatPayMchID == "" || setting.WechatPaySerialNo == "" ||
		setting.WechatPayApiV3Key == "" || setting.WechatPayPrivateKey == "" {
		return nil, errors.New("微信支付未配置")
	}
	sig := configFingerprint(setting.WechatPayMchID, setting.WechatPaySerialNo,
		setting.WechatPayApiV3Key, setting.WechatPayPrivateKey)

	wechatClientMu.Lock()
	defer wechatClientMu.Unlock()
	if wechatClient != nil && wechatClientSig == sig {
		return wechatClient, nil
	}
	client, err := wechat.NewClientV3(setting.WechatPayMchID, setting.WechatPaySerialNo,
		setting.WechatPayApiV3Key, setting.WechatPayPrivateKey)
	if err != nil {
		return nil, err
	}
	// 下载并自动刷新微信平台证书，用于响应及回调验签
	if err := client.AutoVerifySign(); err != nil {
		return nil, err
	}
	wechatClient = client
	wechatClientSig = sig
	return client, nil
}

// NativePrecreate 统一下单，返回可生成二维码的支付串。
// moneyYuan 为实际支付金额（人民币元）。
func NativePrecreate(ctx context.Context, provider, tradeNo, subject string, moneyYuan float64, notifyURL string) (string, error) {
	switch provider {
	case model.PaymentProviderWechatNative:
		client, err := getWechatClient()
		if err != nil {
			return "", err
		}
		totalFen := decimal.NewFromFloat(moneyYuan).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
		if totalFen <= 0 {
			return "", errors.New("支付金额过低")
		}
		bm := make(gopay.BodyMap)
		bm.Set("appid", setting.WechatPayAppID).
			Set("mchid", setting.WechatPayMchID).
			Set("description", subject).
			Set("out_trade_no", tradeNo).
			Set("notify_url", notifyURL).
			// 渠道侧订单截止时间：与本地对账过期判定一致，过期后微信拒绝支付
			Set("time_expire", time.Now().Add(time.Duration(NativeOrderValiditySeconds)*time.Second).Format(time.RFC3339)).
			SetBodyMap("amount", func(b gopay.BodyMap) {
				b.Set("total", totalFen).Set("currency", "CNY")
			})
		rsp, err := client.V3TransactionNative(ctx, bm)
		if err != nil {
			return "", err
		}
		if rsp.Code != wechat.Success || rsp.Response == nil || rsp.Response.CodeUrl == "" {
			return "", fmt.Errorf("微信下单失败: %s", rsp.Error)
		}
		return rsp.Response.CodeUrl, nil
	case model.PaymentProviderAlipayNative:
		client, err := newAlipayClient()
		if err != nil {
			return "", err
		}
		bm := make(gopay.BodyMap)
		bm.Set("out_trade_no", tradeNo).
			Set("total_amount", decimal.NewFromFloat(moneyYuan).Round(2).String()).
			Set("subject", subject).
			Set("notify_url", notifyURL).
			// 渠道侧订单相对超时：与本地对账过期判定一致，过期后支付宝关单拒付
			Set("timeout_express", fmt.Sprintf("%dm", NativeOrderValiditySeconds/60))
		rsp, err := client.TradePrecreate(ctx, bm)
		if err != nil {
			return "", err
		}
		if rsp.Response == nil || rsp.Response.QrCode == "" {
			if rsp.Response != nil {
				return "", fmt.Errorf("支付宝下单失败: %s %s", rsp.Response.SubCode, rsp.Response.SubMsg)
			}
			return "", errors.New("支付宝下单失败")
		}
		return rsp.Response.QrCode, nil
	default:
		return "", fmt.Errorf("未知支付渠道: %s", provider)
	}
}

// NativeQuery 主动查询订单是否支付成功。
func NativeQuery(ctx context.Context, provider, tradeNo string) (bool, error) {
	switch provider {
	case model.PaymentProviderWechatNative:
		client, err := getWechatClient()
		if err != nil {
			return false, err
		}
		rsp, err := client.V3TransactionQueryOrder(ctx, wechat.OutTradeNo, tradeNo)
		if err != nil {
			return false, err
		}
		// 非 200（429/5xx/鉴权失败等）为不确定，必须报错而非当成“未支付”，
		// 否则对账会在瞬时故障下把已支付订单误判过期，造成已付款不到账。
		if rsp.Code != wechat.Success || rsp.Response == nil {
			return false, fmt.Errorf("微信订单查询未成功: %s", rsp.Error)
		}
		return rsp.Response.TradeState == wechat.TradeStateSuccess, nil
	case model.PaymentProviderAlipayNative:
		client, err := newAlipayClient()
		if err != nil {
			return false, err
		}
		bm := make(gopay.BodyMap)
		bm.Set("out_trade_no", tradeNo)
		rsp, err := client.TradeQuery(ctx, bm)
		if err != nil {
			return false, err
		}
		if rsp.Response == nil {
			return false, nil
		}
		st := rsp.Response.TradeStatus
		return st == alipayTradeStatusSuccess || st == alipayTradeStatusFinished, nil
	default:
		return false, fmt.Errorf("未知支付渠道: %s", provider)
	}
}

// NativeRefundSubmit 向渠道发起微信/支付宝原生扫码订单全额退款，返回归一化结果。
// 因 out_refund_no / out_request_no 复用订单号 tradeNo，渠道侧天然幂等（重复调用不重复出款、
// 返回既有退款单当前状态），故本函数既用于首次申请，也用于后台对账的"重新提交/确认"——
// 即使首次申请因请求中断/服务重启从未真正发出，后台再次调用也能补发，不会永久卡在处理中。
// 申请结果不确定（网络错误/非 200）时按 out_refund_no 主动查询确认真实状态，避免误判为失败。
// error 仅用于发起前的配置/参数问题；Processing 表示尚未确认，交由后台对账继续确认。
// refundYuan 为退款金额（元），totalYuan 为订单原金额（元，微信退款必填）。
// fastConfirm=true 时（用户请求内）对微信处理中结果做短暂快速轮询以改善 UX；
// fastConfirm=false 时（后台对账）立即返回处理中，交由下一轮对账，避免长时间持锁。
func NativeRefundSubmit(ctx context.Context, provider, tradeNo, reason string, refundYuan, totalYuan float64, fastConfirm bool) (RefundOutcome, error) {
	switch provider {
	case model.PaymentProviderWechatNative:
		client, err := getWechatClient()
		if err != nil {
			return RefundOutcomeProcessing, err
		}
		refundFen := decimal.NewFromFloat(refundYuan).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
		totalFen := decimal.NewFromFloat(totalYuan).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
		if refundFen <= 0 || totalFen <= 0 || refundFen > totalFen {
			return RefundOutcomeProcessing, errors.New("退款金额无效")
		}
		bm := make(gopay.BodyMap)
		bm.Set("out_trade_no", tradeNo).
			Set("out_refund_no", tradeNo).
			Set("reason", reason).
			SetBodyMap("amount", func(b gopay.BodyMap) {
				b.Set("refund", refundFen).Set("total", totalFen).Set("currency", "CNY")
			})
		rsp, err := client.V3Refund(ctx, bm)
		if err != nil || rsp.Code != wechat.Success || rsp.Response == nil {
			// 申请结果不确定：按 out_refund_no 查询确认（退款可能已受理）
			return wechatRefundOutcomeByQuery(ctx, client, tradeNo), nil
		}
		outcome := interpretWechatRefundStatus(rsp.Response.Status)
		if outcome == RefundOutcomeProcessing && fastConfirm {
			// 常见即时到账：请求内做短暂快速轮询以改善 UX；未确认则留给后台对账
			return waitWechatRefundOutcome(ctx, client, tradeNo), nil
		}
		return outcome, nil
	case model.PaymentProviderAlipayNative:
		client, err := newAlipayClient()
		if err != nil {
			return RefundOutcomeProcessing, err
		}
		bm := make(gopay.BodyMap)
		bm.Set("out_trade_no", tradeNo).
			Set("out_request_no", tradeNo).
			Set("refund_amount", decimal.NewFromFloat(refundYuan).Round(2).String())
		if reason != "" {
			bm.Set("refund_reason", reason)
		}
		// 支付宝退款为同步：err==nil 即到账成功；否则按 out_request_no 查询确认真实状态
		if _, err := client.TradeRefund(ctx, bm); err != nil {
			return alipayRefundOutcomeByQuery(ctx, client, tradeNo), nil
		}
		return RefundOutcomeSuccess, nil
	default:
		return RefundOutcomeProcessing, fmt.Errorf("未知支付渠道: %s", provider)
	}
}

// wechatRefundOutcomeByQuery 单次按 out_refund_no 查询微信退款结果；查询失败视为处理中。
func wechatRefundOutcomeByQuery(ctx context.Context, client *wechat.ClientV3, tradeNo string) RefundOutcome {
	rsp, err := client.V3RefundQuery(ctx, tradeNo, nil)
	if err != nil || rsp.Code != wechat.Success || rsp.Response == nil {
		return RefundOutcomeProcessing
	}
	return interpretWechatRefundStatus(rsp.Response.Status)
}

// waitWechatRefundOutcome 请求内短暂快速轮询微信退款查询（UX 优化）。遵守 ctx 取消；
// 窗口内未得到终态则返回 Processing，交由后台对账继续确认。
func waitWechatRefundOutcome(ctx context.Context, client *wechat.ClientV3, tradeNo string) RefundOutcome {
	for i := 0; i < nativeRefundFastPolls; i++ {
		select {
		case <-ctx.Done():
			return RefundOutcomeProcessing
		case <-time.After(nativeRefundPollInterval):
		}
		if outcome := wechatRefundOutcomeByQuery(ctx, client, tradeNo); outcome != RefundOutcomeProcessing {
			return outcome
		}
	}
	return RefundOutcomeProcessing
}

// alipayRefundOutcomeByQuery 按 out_request_no 查询支付宝退款结果；查询失败/未成功视为处理中。
func alipayRefundOutcomeByQuery(ctx context.Context, client *alipay.Client, tradeNo string) RefundOutcome {
	bm := make(gopay.BodyMap)
	bm.Set("out_trade_no", tradeNo).Set("out_request_no", tradeNo)
	rsp, err := client.TradeFastPayRefundQuery(ctx, bm)
	if err != nil {
		return RefundOutcomeProcessing
	}
	if rsp.Response != nil && rsp.Response.RefundStatus == alipayRefundStatusSuccess {
		return RefundOutcomeSuccess
	}
	return RefundOutcomeProcessing
}

// interpretWechatRefundStatus 把微信退款单状态归一化为 RefundOutcome。
func interpretWechatRefundStatus(status string) RefundOutcome {
	switch status {
	case wechatRefundStatusSuccess:
		return RefundOutcomeSuccess
	case wechatRefundStatusClosed:
		return RefundOutcomeClosed
	case wechatRefundStatusAbnormal:
		return RefundOutcomeAbnormal
	default:
		// PROCESSING 及未知状态一律按处理中，交由后台对账继续确认
		return RefundOutcomeProcessing
	}
}

// VerifyWechatNotify 验证微信支付异步回调：验签 + AES-GCM 解密，返回订单号与是否支付成功。
func VerifyWechatNotify(req *http.Request) (tradeNo string, paid bool, err error) {
	client, err := getWechatClient()
	if err != nil {
		return "", false, err
	}
	notifyReq, err := wechat.V3ParseNotify(req)
	if err != nil {
		return "", false, err
	}
	if err := notifyReq.VerifySignByPKMap(client.WxPublicKeyMap()); err != nil {
		return "", false, err
	}
	result, err := notifyReq.DecryptPayCipherText(setting.WechatPayApiV3Key)
	if err != nil {
		return "", false, err
	}
	return result.OutTradeNo, result.TradeState == wechat.TradeStateSuccess, nil
}

// VerifyAlipayNotify 验证支付宝异步回调（公钥模式验签），返回订单号与是否支付成功。
func VerifyAlipayNotify(req *http.Request) (tradeNo string, paid bool, err error) {
	if setting.AlipayPublicKey == "" {
		return "", false, errors.New("支付宝公钥未配置")
	}
	bm, err := alipay.ParseNotifyToBodyMap(req)
	if err != nil {
		return "", false, err
	}
	ok, err := alipay.VerifySign(setting.AlipayPublicKey, bm)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, errors.New("支付宝回调验签失败")
	}
	status := bm.GetString("trade_status")
	return bm.GetString("out_trade_no"), status == alipayTradeStatusSuccess || status == alipayTradeStatusFinished, nil
}
