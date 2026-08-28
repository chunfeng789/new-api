package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCloudflareWebAnalyticsHostTokens(t *testing.T) {
	hostTokens := parseCloudflareWebAnalyticsHostTokens(
		"GPTTalkApp.com.=main-token, gpttalk.cc=mirror-token,invalid,missing-token=,=missing-host",
	)

	assert.Equal(t, map[string]string{
		"gpttalkapp.com": "main-token",
		"gpttalk.cc":     "mirror-token",
	}, hostTokens)
}
