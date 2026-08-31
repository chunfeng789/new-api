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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseEmailSuffixes(tc.value))
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
