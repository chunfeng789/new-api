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

// normalizeEmailSuffix 规范化单个配置后缀：去空格、转小写、去掉可选的前导 "@" 或 "."
// （管理员很自然会写成 "@gmail.com"），并校验其为合法域名后缀（仅含字母、数字、"." 与
// "-"，无空标签或前后分隔符）。非法输入返回 ""，便于调用方直接丢弃，从而保证
// “已启用限制 ⇒ 至少一个有效后缀”不会被空白、"@gmail.com" 之外的非域名值静默绕过。
func normalizeEmailSuffix(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "@")
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "-") ||
		strings.HasPrefix(s, "-") || strings.Contains(s, "..") {
		return ""
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
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

// RejectInviteRewardChange 校验邀请奖励配置变更是否会破坏“已启用限制 ⇒ 至少一个有效
// 后缀”的不变量：允许则返回空串，否则返回面向管理员的中文拒绝原因。当前状态由调用方在
// 串行化的临界区内传入，以避免检查与写入之间的竞态（TOCTOU）。
func RejectInviteRewardChange(key, value string, currentEnabled bool, currentSuffixCount int) string {
	switch key {
	case "InviteRewardEmailRestrictionEnabled":
		if value == "true" && currentSuffixCount == 0 {
			return "无法启用邀请奖励邮箱后缀限制，请先填入允许的邮箱后缀！"
		}
	case "InviteRewardEmailSuffixes":
		if currentEnabled && len(ParseEmailSuffixes(value)) == 0 {
			return "邀请奖励邮箱后缀限制已启用，请至少保留一个有效的邮箱后缀！"
		}
	}
	return ""
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
