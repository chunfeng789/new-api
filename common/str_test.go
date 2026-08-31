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
		{"drops a label ending in a hyphen", "foo-.com", []string{}},
		{"drops a label starting with a hyphen", "foo.-bar.com", []string{}},
		{"keeps a bare tld for whole-tld matching", "edu", []string{"edu"}},
		{"keeps hyphenated labels", "mail-server.co.uk", []string{"mail-server.co.uk"}},
		{"keeps valid and drops invalid in one list", "@gmail.com, bad@x , outlook.com", []string{"gmail.com", "outlook.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseEmailSuffixes(tc.value))
		})
	}
}

// TestNormalizeEmailSuffixesStrict guards that a save collects every non-empty
// invalid entry (so the caller can reject rather than silently drop it) and
// returns the normalized, de-duplicated value to persist.
func TestNormalizeEmailSuffixesStrict(t *testing.T) {
	t.Run("normalizes, dedupes and reports no invalid for a clean list", func(t *testing.T) {
		normalized, invalid := NormalizeEmailSuffixesStrict("@Gmail.com, gmail.com , outlook.com")
		assert.Equal(t, "gmail.com,outlook.com", normalized)
		assert.Empty(t, invalid)
	})
	t.Run("collects non-empty invalid entries verbatim", func(t *testing.T) {
		normalized, invalid := NormalizeEmailSuffixesStrict("gmail.com, bad@x , foo-.com")
		assert.Equal(t, "gmail.com", normalized)
		assert.Equal(t, []string{"bad@x", "foo-.com"}, invalid)
	})
	t.Run("ignores blank entries without flagging them", func(t *testing.T) {
		normalized, invalid := NormalizeEmailSuffixesStrict(" , gmail.com , ")
		assert.Equal(t, "gmail.com", normalized)
		assert.Empty(t, invalid)
	})
}

// TestValidateInviteRewardConfig guards the atomic pair validation: it rejects
// any non-empty invalid suffix, rejects enabling with no valid suffix, and
// otherwise returns the normalized value to persist.
func TestValidateInviteRewardConfig(t *testing.T) {
	cases := []struct {
		name           string
		enabled        bool
		suffixes       string
		wantNormalized string
		wantRejected   bool
	}{
		{"reject enabling with empty suffixes", true, "", "", true},
		{"reject enabling with only-blank suffixes", true, " , ", "", true},
		{"reject any non-empty invalid entry", true, "gmail.com, bad@x", "gmail.com", true},
		{"reject invalid even while disabled", false, "foo-.com", "", true},
		{"allow enabling with a valid normalized suffix", true, "@Gmail.com", "gmail.com", false},
		{"allow disabling with empty suffixes", false, "", "", false},
		{"normalizes and dedupes on success", true, "gmail.com, GMAIL.com , outlook.com", "gmail.com,outlook.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalized, msg := ValidateInviteRewardConfig(tc.enabled, tc.suffixes)
			assert.Equal(t, tc.wantNormalized, normalized)
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
