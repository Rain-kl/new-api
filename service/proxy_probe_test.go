package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProxyQualityGrade(t *testing.T) {
	assert.Equal(t, "A", ProxyQualityGrade(90))
	assert.Equal(t, "B", ProxyQualityGrade(75))
	assert.Equal(t, "C", ProxyQualityGrade(60))
	assert.Equal(t, "D", ProxyQualityGrade(40))
	assert.Equal(t, "F", ProxyQualityGrade(10))
}

func TestFinalizeProxyQualityResult(t *testing.T) {
	r := &ProxyQualityCheckResult{PassedCount: 2, WarnCount: 1, FailedCount: 1, ChallengeCount: 0}
	FinalizeProxyQualityResult(r)
	assert.Equal(t, 100-10-22, r.Score)
	assert.Equal(t, "C", r.Grade) // 68
	assert.Equal(t, "failed", r.Status)
	assert.False(t, r.Success)
	assert.Contains(t, r.Summary, "通过 2 项")
}
