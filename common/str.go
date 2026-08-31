package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"strconv"
	"strings"
	"unsafe"

	"github.com/samber/lo"
)

const LocalLogContentLimit = 2048

// LocalLogPreview limits log-only content unless debug logging is enabled.
func LocalLogPreview(content string) string {
	if DebugEnabled || len(content) <= LocalLogContentLimit {
		return content
	}
	return fmt.Sprintf("%s... [truncated, original_length=%d, limit=%d]", content[:LocalLogContentLimit], len(content), LocalLogContentLimit)
}

func GetStringIfEmpty(str string, defaultValue string) string {
	if str == "" {
		return defaultValue
	}
	return str
}

func GetRandomString(length int) string {
	if length <= 0 {
		return ""
	}
	return lo.RandomString(length, lo.AlphanumericCharset)
}

func MapToJsonStr(m map[string]interface{}) string {
	bytes, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func StrToMap(str string) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := Unmarshal([]byte(str), &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func StrToJsonArray(str string) ([]interface{}, error) {
	var js []interface{}
	err := json.Unmarshal([]byte(str), &js)
	if err != nil {
		return nil, err
	}
	return js, nil
}

func IsJsonArray(str string) bool {
	var js []interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

func IsJsonObject(str string) bool {
	var js map[string]interface{}
	return json.Unmarshal([]byte(str), &js) == nil
}

func String2Int(str string) int {
	num, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return num
}

func StringsContains(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}

// StringToByteSlice []byte only read, panic on append
func StringToByteSlice(s string) []byte {
	tmp1 := (*[2]uintptr)(unsafe.Pointer(&s))
	tmp2 := [3]uintptr{tmp1[0], tmp1[1], tmp1[1]}
	return *(*[]byte)(unsafe.Pointer(&tmp2))
}

func EncodeBase64(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func GetJsonString(data any) string {
	if data == nil {
		return ""
	}
	b, _ := json.Marshal(data)
	return string(b)
}

// NormalizeBillingPreference clamps the billing preference to valid values.
func NormalizeBillingPreference(pref string) string {
	switch strings.TrimSpace(pref) {
	case "subscription_first", "wallet_first", "subscription_only", "wallet_only":
		return strings.TrimSpace(pref)
	default:
		return "subscription_first"
	}
}

// isValidDNSLabel 校验单个 DNS label：长度 1..63，仅含字母、数字和连字符，且不以
// 连字符开头或结尾。
func isValidDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// normalizeEmailSuffix 规范化单个配置后缀：去空格、转小写、去掉可选的前导 "@" 或 "."
// （管理员很自然会写成 "@gmail.com"），并按 DNS label 规则逐段校验（总长 ≤253，每个
// label 1..63 且不以连字符开头/结尾），因此 "foo-.com"、"foo.-bar.com" 等非法域名会被拒绝。
// 非法输入返回 ""，便于调用方直接丢弃或拒绝，从而保证“已启用限制 ⇒ 至少一个有效后缀”
// 不会被非域名格式的非空值绕过。
func normalizeEmailSuffix(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimLeft(s, ".")
	if s == "" || len(s) > 253 {
		return ""
	}
	for _, label := range strings.Split(s, ".") {
		if !isValidDNSLabel(label) {
			return ""
		}
	}
	return s
}

// ParseEmailSuffixes 将逗号分隔的后缀名单解析为规范化、丢弃非法项后的合法后缀切片，
// 作为邀请奖励邮箱后缀名单的唯一解析入口，保证存储配置与校验逻辑一致。
func ParseEmailSuffixes(value string) []string {
	parts := strings.Split(value, ",")
	suffixes := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := normalizeEmailSuffix(part); s != "" {
			suffixes = append(suffixes, s)
		}
	}
	return suffixes
}

// NormalizeEmailSuffixesStrict 解析逗号分隔的后缀名单，返回规范化、去重后的逗号拼接结果，
// 以及所有“非空但格式非法”的原始项。调用方应在 invalid 非空时整体拒绝更新，而不是静默丢弃
// 非法项后仍以原始字符串持久化——那会导致数据库、管理页面与运行时规则不一致。
func NormalizeEmailSuffixesStrict(value string) (normalized string, invalid []string) {
	seen := make(map[string]bool)
	valid := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		n := normalizeEmailSuffix(trimmed)
		if n == "" {
			invalid = append(invalid, trimmed)
			continue
		}
		if !seen[n] {
			seen[n] = true
			valid = append(valid, n)
		}
	}
	return strings.Join(valid, ","), invalid
}

// InviteRewardEmailAllowed 判断新注册用户的邮箱在后缀限制下是否有资格获得邀请奖励。
// 关闭时始终返回 true（向后兼容）；开启时先在 OptionMapRWMutex 读锁下取配置快照，
// 再对每个后缀做带 "@"/"." 边界的匹配，避免 notgmail.com 命中 gmail.com，并避免与
// 后台配置更新并发时产生 data race。
func InviteRewardEmailAllowed(email string) bool {
	OptionMapRWMutex.RLock()
	enabled := InviteRewardEmailRestrictionEnabled
	suffixes := InviteRewardEmailSuffixes
	OptionMapRWMutex.RUnlock()

	if !enabled {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	for _, suffix := range suffixes {
		s := normalizeEmailSuffix(suffix)
		if s == "" {
			continue
		}
		if strings.HasSuffix(email, "@"+s) || strings.HasSuffix(email, "."+s) {
			return true
		}
	}
	return false
}

// ValidateInviteRewardConfig 校验邀请奖励配置对（开关 + 后缀名单）的一次原子提交：返回
// 规范化、去重后应持久化的后缀字符串，以及面向管理员的中文拒绝原因（为空表示通过）。
// 校验作用在“将要写入的完整配置对”上，从而在多实例部署中也不会出现“已启用但后缀为空”的
// 非法中间态；含非空非法项则整体拒绝，避免静默丢弃与展示/运行不一致。
func ValidateInviteRewardConfig(enabled bool, suffixes string) (normalized string, rejectMsg string) {
	normalized, invalid := NormalizeEmailSuffixesStrict(suffixes)
	if len(invalid) > 0 {
		return normalized, "以下邮箱后缀格式非法，请修正后再保存：" + strings.Join(invalid, "、")
	}
	if enabled && normalized == "" {
		return normalized, "启用邀请奖励邮箱后缀限制时，请至少填写一个有效的邮箱后缀！"
	}
	return normalized, ""
}

// MaskEmail masks a user email to prevent PII leakage in logs
// Returns "***masked***" if email is empty, otherwise shows only the domain part
func MaskEmail(email string) string {
	if email == "" {
		return "***masked***"
	}

	// Find the @ symbol
	atIndex := strings.Index(email, "@")
	if atIndex == -1 {
		// No @ symbol found, return masked
		return "***masked***"
	}

	// Return only the domain part with @ symbol
	return "***@" + email[atIndex+1:]
}

// MaskSensitiveInfo moved to the conversion kit (kitutil) because the types
// package error formatting depends on it; host callers keep this name.
func MaskSensitiveInfo(str string) string {
	return kitutil.MaskSensitiveInfo(str)
}
