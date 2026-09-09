package oauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectGitHubEmail(t *testing.T) {
	cases := []struct {
		name   string
		emails []gitHubUserEmail
		want   string
	}{
		{
			name:   "no emails",
			emails: nil,
			want:   "",
		},
		{
			name: "primary verified wins over an earlier verified one",
			emails: []gitHubUserEmail{
				{Email: "secondary@example.com", Primary: false, Verified: true},
				{Email: "primary@example.com", Primary: true, Verified: true},
			},
			want: "primary@example.com",
		},
		{
			name: "unverified primary falls back to a verified address",
			emails: []gitHubUserEmail{
				{Email: "unverified@example.com", Primary: true, Verified: false},
				{Email: "verified@example.com", Primary: false, Verified: true},
			},
			want: "verified@example.com",
		},
		{
			name: "every address unverified",
			emails: []gitHubUserEmail{
				{Email: "unverified@example.com", Primary: true, Verified: false},
			},
			want: "",
		},
		{
			name: "noreply alias is skipped even when primary",
			emails: []gitHubUserEmail{
				{Email: "12345+octocat@users.noreply.github.com", Primary: true, Verified: true},
				{Email: "octocat@example.com", Primary: false, Verified: true},
			},
			want: "octocat@example.com",
		},
		{
			name: "noreply alias is the only address",
			emails: []gitHubUserEmail{
				{Email: "12345+octocat@USERS.NOREPLY.GITHUB.COM", Primary: true, Verified: true},
			},
			want: "",
		},
		{
			name: "blank entries are ignored",
			emails: []gitHubUserEmail{
				{Email: "   ", Primary: true, Verified: true},
				{Email: " octocat@example.com ", Primary: false, Verified: true},
			},
			want: "octocat@example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, selectGitHubEmail(tc.emails))
		})
	}
}
