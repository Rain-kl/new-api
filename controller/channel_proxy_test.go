package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupChannelProxyTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:channel_proxy_%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	require.NoError(t, db.AutoMigrate(&model.Proxy{}, &model.Channel{}))
}

func TestNormalizeChannelProxySettings_Managed(t *testing.T) {
	setupChannelProxyTestDB(t)
	p := &model.Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())

	s := dto.ChannelSettings{ProxyId: p.Id, Proxy: "should-be-overwritten"}
	require.NoError(t, normalizeChannelProxySettings(&s))
	assert.Equal(t, p.URL(), s.Proxy)
	assert.Equal(t, p.Id, s.ProxyId)
}

func TestNormalizeChannelProxySettings_CustomURL(t *testing.T) {
	s := dto.ChannelSettings{ProxyId: 0, Proxy: "socks5://h:1080"}
	require.NoError(t, normalizeChannelProxySettings(&s))
	assert.Equal(t, "socks5://h:1080", s.Proxy)
	assert.Equal(t, 0, s.ProxyId)
}

func TestNormalizeChannelProxySettings_InvalidManaged(t *testing.T) {
	setupChannelProxyTestDB(t)
	s := dto.ChannelSettings{ProxyId: 99999}
	require.Error(t, normalizeChannelProxySettings(&s))
}

func TestNormalizeChannelProxySettings_InactiveManaged(t *testing.T) {
	setupChannelProxyTestDB(t)
	p := &model.Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080, Status: model.ProxyStatusInactive}
	require.NoError(t, p.Insert())
	s := dto.ChannelSettings{ProxyId: p.Id}
	require.Error(t, normalizeChannelProxySettings(&s))
}
