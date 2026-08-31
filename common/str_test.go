package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseEmailSuffixes guards that the canonical suffix parser normalizes the
// stored allowlist (trim, lower-case, drop blanks) so that the "enabled implies
// at least one valid suffix" guard cannot be bypassed with blank or "," input.
func TestParseEmailSuffixes(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty string yields empty slice", "", []string{}},
		{"comma only yields empty slice", ",", []string{}},
		{"blanks are dropped", " , ,  ", []string{}},
		{"trims and lower-cases", " Gmail.com , OUTLOOK.com ", []string{"gmail.com", "outlook.com"}},
		{"keeps valid entries and drops blanks", "gmail.com,,outlook.com", []string{"gmail.com", "outlook.com"}},
		{"strips a leading @ that admins naturally type", "@gmail.com", []string{"gmail.com"}},
		{"strips a leading dot", ".gmail.com", []string{"gmail.com"}},
		{"drops values with an embedded @", "user@gmail.com", []string{}},
		{"drops values containing spaces", "gmail com", []string{}},
		{"drops values with empty labels", "gmail..com", []string{}},
		{"drops trailing-dot values", "gmail.com.", []string{}},
		{"keeps a bare tld for whole-tld matching", "edu", []string{"edu"}},
		{"keeps valid and drops invalid in one list", "@gmail.com, bad@x , outlook.com", []string{"gmail.com", "outlook.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseEmailSuffixes(tc.value))
		})
	}
}

// TestRejectInviteRewardChange guards the cross-config invariant "restriction
// enabled implies at least one valid suffix": enabling with an empty list and
// clearing the list while enabled are both rejected, and a non-domain-only list
// (e.g. "@gmail.com") is normalized to a valid suffix so enabling is allowed.
func TestRejectInviteRewardChange(t *testing.T) {
	cases := []struct {
		name         string
		key          string
		value        string
		curEnabled   bool
		curSuffixCnt int
		wantRejected bool
	}{
		{"reject enabling with empty suffix list", "InviteRewardEmailRestrictionEnabled", "true", false, 0, true},
		{"allow enabling with suffixes present", "InviteRewardEmailRestrictionEnabled", "true", false, 1, false},
		{"allow disabling regardless of suffixes", "InviteRewardEmailRestrictionEnabled", "false", true, 0, false},
		{"reject clearing suffixes while enabled", "InviteRewardEmailSuffixes", "", true, 1, true},
		{"reject clearing to only-invalid while enabled", "InviteRewardEmailSuffixes", " , bad@x ", true, 1, true},
		{"allow clearing suffixes while disabled", "InviteRewardEmailSuffixes", "", false, 1, false},
		{"allow setting valid suffixes while enabled", "InviteRewardEmailSuffixes", "gmail.com", true, 1, false},
		{"allow @-prefixed suffix while enabled", "InviteRewardEmailSuffixes", "@gmail.com", true, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := RejectInviteRewardChange(tc.key, tc.value, tc.curEnabled, tc.curSuffixCnt)
			assert.Equal(t, tc.wantRejected, msg != "")
		})
	}
}

// TestInviteRewardEmailAllowed guards the invite-reward eligibility boundary:
// when the suffix restriction is off everyone qualifies (backward compatible),
// and when it is on only emails whose domain matches a configured suffix (with
// an "@"/"." boundary so notgmail.com does not match gmail.com) qualify.
func TestInviteRewardEmailAllowed(t *testing.T) {
	origEnabled := InviteRewardEmailRestrictionEnabled
	origSuffixes := InviteRewardEmailSuffixes
	t.Cleanup(func() {
		InviteRewardEmailRestrictionEnabled = origEnabled
		InviteRewardEmailSuffixes = origSuffixes
	})

	cases := []struct {
		name     string
		enabled  bool
		suffixes []string
		email    string
		want     bool
	}{
		{"disabled allows non-matching email", false, []string{"gmail.com"}, "a@example.com", true},
		{"disabled allows empty email", false, []string{"gmail.com"}, "", true},
		{"enabled rejects empty email", true, []string{"gmail.com"}, "", false},
		{"enabled matches exact domain", true, []string{"gmail.com"}, "user@gmail.com", true},
		{"enabled matches subdomain", true, []string{"gmail.com"}, "user@mail.gmail.com", true},
		{"enabled rejects look-alike domain", true, []string{"gmail.com"}, "user@notgmail.com", false},
		{"enabled is case-insensitive", true, []string{"gmail.com"}, "User@GMAIL.COM", true},
		{"enabled trims and skips blank entries", true, []string{" ", "gmail.com "}, "user@gmail.com", true},
		{"enabled normalizes a stored @-prefixed suffix", true, []string{"@gmail.com"}, "user@gmail.com", true},
		{"enabled skips a stored invalid suffix", true, []string{"bad@x", "gmail.com"}, "user@gmail.com", true},
		{"enabled rejects domain not in list", true, []string{"outlook.com"}, "user@gmail.com", false},
		{"enabled with empty list rejects", true, []string{}, "user@gmail.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			InviteRewardEmailRestrictionEnabled = tc.enabled
			InviteRewardEmailSuffixes = tc.suffixes
			assert.Equal(t, tc.want, InviteRewardEmailAllowed(tc.email))
		})
	}
}
