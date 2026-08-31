package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-pay/gopay/alipay"
	"github.com/stretchr/testify/assert"
)

// 退款错误的「确定性拒绝 vs 可重试」分类是账目一致性的关键契约：只有确定性业务拒绝才可转人工
// (refund_failed) 并退出对账；系统错误/网络/未知错误必须按可重试处理，否则已受理但暂不可见的退款
// 会被误判为永久失败，导致渠道已退款而本地额度未扣回。

func TestIsTerminalAlipayRefundReject(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"trade_not_exist_terminal", &alipay.BizErr{Code: "40004", SubCode: "ACQ.TRADE_NOT_EXIST"}, true},
		{"trade_status_error_terminal", &alipay.BizErr{Code: "40004", SubCode: "ACQ.TRADE_STATUS_ERROR"}, true},
		{"refund_amt_mismatch_terminal", &alipay.BizErr{Code: "40004", SubCode: "ACQ.REFUND_AMT_NOT_EQUAL_TOTAL"}, true},
		// 顶层码同为 40004 但 sub_code 为系统错误：必须按可重试处理（这是本轮修复的核心回归点）
		{"system_error_retryable", &alipay.BizErr{Code: "40004", SubCode: "ACQ.SYSTEM_ERROR"}, false},
		{"gateway_busy_retryable", &alipay.BizErr{Code: "20000", SubCode: ""}, false},
		{"unknown_subcode_retryable", &alipay.BizErr{Code: "40004", SubCode: "ACQ.SOMETHING_UNLISTED"}, false},
		{"seller_balance_not_enough_retryable", &alipay.BizErr{Code: "40004", SubCode: "ACQ.SELLER_BALANCE_NOT_ENOUGH"}, false},
		{"non_biz_network_error_retryable", errors.New("connection reset by peer"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTerminalAlipayRefundReject(tc.err))
		})
	}
}

func TestIsTerminalWechatRefundReject(t *testing.T) {
	cases := []struct {
		name string
		code int
		want bool
	}{
		{"bad_request_terminal", http.StatusBadRequest, true},
		{"forbidden_terminal", http.StatusForbidden, true},
		{"not_found_terminal", http.StatusNotFound, true},
		{"too_many_requests_retryable", http.StatusTooManyRequests, false},
		{"internal_error_retryable", http.StatusInternalServerError, false},
		{"bad_gateway_retryable", http.StatusBadGateway, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTerminalWechatRefundReject(tc.code))
		})
	}
}
