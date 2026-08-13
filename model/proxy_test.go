package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupProxyTestDB(t *testing.T) {
	t.Helper()
	// Save and restore the package-global DB handles: this fixture replaces
	// model.DB/LOG_DB with a minimal in-memory DB (Proxy + Channel only), and
	// tests running later in the same process rely on the full schema (users,
	// logs, ...). Without the restore, they fail with "no such table: users".
	previousDB, previousLogDB := DB, LOG_DB
	previousMainDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		common.RedisEnabled = previousRedisEnabled
	})

	dsn := fmt.Sprintf("file:proxy_%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	require.NoError(t, db.AutoMigrate(&Proxy{}, &Channel{}))
}

func TestProxyURL_NoAuth(t *testing.T) {
	p := &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
	assert.Equal(t, "http://proxy.example:8080", p.URL())
}

func TestProxyURL_WithAuth(t *testing.T) {
	p := &Proxy{Protocol: "socks5", Host: "1.2.3.4", Port: 1080, Username: "u", Password: "p"}
	assert.Equal(t, "socks5://u:p@1.2.3.4:1080", p.URL())
}

func TestProxyURL_UsernameOnlyNoAuth(t *testing.T) {
	p := &Proxy{Protocol: "http", Host: "h", Port: 1, Username: "u"}
	assert.Equal(t, "http://h:1", p.URL())
}

func TestProxyValidate_RequiresBackupWhenFallbackProxy(t *testing.T) {
	p := &Proxy{Name: "a", Protocol: "http", Host: "h", Port: 8080, FallbackMode: ProxyFallbackProxy}
	require.Error(t, p.Validate())
}

func TestProxyValidate_BackupCannotBeSelf(t *testing.T) {
	p := &Proxy{Id: 3, Name: "a", Protocol: "http", Host: "h", Port: 8080, FallbackMode: ProxyFallbackProxy, BackupProxyId: 3}
	require.Error(t, p.Validate())
}

func TestProxyValidate_OK(t *testing.T) {
	p := &Proxy{Name: "a", Protocol: "https", Host: "h", Port: 443, FallbackMode: ProxyFallbackNone}
	require.NoError(t, p.Validate())
	assert.Equal(t, ProxyStatusActive, p.Status)
	assert.Equal(t, 7, p.ExpiryWarnDays)
}

func TestProxyInsertAndGet(t *testing.T) {
	setupProxyTestDB(t)
	p := &Proxy{Name: "n1", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())
	got, err := GetProxyById(p.Id)
	require.NoError(t, err)
	assert.Equal(t, "n1", got.Name)
	assert.Equal(t, ProxyStatusActive, got.Status)
}

func TestProxyListFilterProtocol(t *testing.T) {
	setupProxyTestDB(t)
	require.NoError(t, (&Proxy{Name: "a", Protocol: "http", Host: "h", Port: 1}).Insert())
	require.NoError(t, (&Proxy{Name: "b", Protocol: "socks5", Host: "h", Port: 2}).Insert())
	list, total, err := ListProxies(0, 10, ProxyListFilters{Protocol: "socks5"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "b", list[0].Name)
}

func TestCheckProxyExists(t *testing.T) {
	setupProxyTestDB(t)
	require.NoError(t, (&Proxy{Name: "a", Protocol: "http", Host: "h", Port: 9, Username: "u", Password: "p"}).Insert())
	ok, err := CheckProxyExists("h", 9, "u", "p")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestSetChannelsProxyAndCount(t *testing.T) {
	setupProxyTestDB(t)
	p := &Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())
	ch := &Channel{Name: "c", Key: "k", Status: 1}
	require.NoError(t, DB.Create(ch).Error)

	n, err := SetChannelsProxy([]int{ch.Id}, p.Id, p.URL())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	count, err := CountChannelsByProxyID(p.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	s := reloaded.GetSetting()
	assert.Equal(t, p.Id, s.ProxyId)
	assert.Equal(t, p.URL(), s.Proxy)
}

func TestListChannelsForProxyBinding_BoundFirstThenUnbound(t *testing.T) {
	setupProxyTestDB(t)
	p := &Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())

	ch1 := &Channel{Name: "c1", Key: "k1", Status: 1}
	ch2 := &Channel{Name: "c2", Key: "k2", Status: 1}
	ch3 := &Channel{Name: "c3", Key: "k3", Status: 1}
	require.NoError(t, DB.Create(ch1).Error)
	require.NoError(t, DB.Create(ch2).Error)
	require.NoError(t, DB.Create(ch3).Error)

	// Bind ch1 and ch3 to the proxy; ch2 stays unbound.
	n, err := SetChannelsProxy([]int{ch1.Id, ch3.Id}, p.Id, p.URL())
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	rows, err := ListChannelsForProxyBinding(p.Id)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	// Every channel is listed; bound channels come first (by id), then unbound.
	assert.Equal(t, []int{ch1.Id, ch3.Id, ch2.Id},
		[]int{rows[0].Id, rows[1].Id, rows[2].Id})
	assert.True(t, rows[0].Bound)
	assert.True(t, rows[1].Bound)
	assert.False(t, rows[2].Bound)
}

func TestListChannelsForProxyBinding_UnboundProxy(t *testing.T) {
	setupProxyTestDB(t)
	ch := &Channel{Name: "c", Key: "k", Status: 1}
	require.NoError(t, DB.Create(ch).Error)

	// A proxy id that binds nothing still lists all channels as unbound.
	rows, err := ListChannelsForProxyBinding(999)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.False(t, rows[0].Bound)
}

func TestApplyProxyURLToChannels(t *testing.T) {
	setupProxyTestDB(t)
	p := &Proxy{Name: "p", Protocol: "http", Host: "h", Port: 8080}
	require.NoError(t, p.Insert())
	ch := &Channel{Name: "c", Key: "k", Status: 1}
	require.NoError(t, DB.Create(ch).Error)
	_, err := SetChannelsProxy([]int{ch.Id}, p.Id, p.URL())
	require.NoError(t, err)

	oldURLs, err := ApplyProxyURLToChannels(p.Id, "http://new-host:9090")
	require.NoError(t, err)
	assert.Contains(t, oldURLs, p.URL())

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	s := reloaded.GetSetting()
	assert.Equal(t, p.Id, s.ProxyId)
	assert.Equal(t, "http://new-host:9090", s.Proxy)
}

func TestResolveProxyFallback_Direct(t *testing.T) {
	p := &Proxy{FallbackMode: ProxyFallbackDirect}
	target, clear, err := ResolveProxyFallback(p, common.GetTimestamp(), map[int]struct{}{})
	require.NoError(t, err)
	assert.True(t, clear)
	assert.Nil(t, target)
}

func TestResolveProxyFallback_ProxyChain(t *testing.T) {
	setupProxyTestDB(t)
	backup := &Proxy{Name: "b", Protocol: "http", Host: "b", Port: 1, Status: ProxyStatusActive}
	require.NoError(t, backup.Insert())
	primary := &Proxy{Name: "a", Protocol: "http", Host: "a", Port: 2, FallbackMode: ProxyFallbackProxy, BackupProxyId: backup.Id}
	target, clear, err := ResolveProxyFallback(primary, common.GetTimestamp(), map[int]struct{}{})
	require.NoError(t, err)
	assert.False(t, clear)
	require.NotNil(t, target)
	assert.Equal(t, backup.Id, target.Id)
}

func TestProxyKeyStable(t *testing.T) {
	assert.Equal(t, "http|h|1|u|p", ProxyKey("HTTP", "h", 1, "u", "p"))
}

func TestSweepExpiredProxies_RebindsChannels(t *testing.T) {
	setupProxyTestDB(t)
	backup := &Proxy{Name: "b", Protocol: "http", Host: "backup", Port: 1, Status: ProxyStatusActive}
	require.NoError(t, backup.Insert())
	primary := &Proxy{
		Name:          "a",
		Protocol:      "http",
		Host:          "primary",
		Port:          2,
		Status:        ProxyStatusActive,
		ExpiresAt:     common.GetTimestamp() - 10,
		FallbackMode:  ProxyFallbackProxy,
		BackupProxyId: backup.Id,
	}
	require.NoError(t, primary.Insert())

	ch := &Channel{Name: "c", Key: "k", Status: 1}
	require.NoError(t, DB.Create(ch).Error)
	_, err := SetChannelsProxy([]int{ch.Id}, primary.Id, primary.URL())
	require.NoError(t, err)

	n, err := SweepExpiredProxies()
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	got, err := GetProxyById(primary.Id)
	require.NoError(t, err)
	assert.Equal(t, ProxyStatusExpired, got.Status)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, ch.Id).Error)
	s := reloaded.GetSetting()
	assert.Equal(t, backup.Id, s.ProxyId)
	assert.Equal(t, backup.URL(), s.Proxy)
}
