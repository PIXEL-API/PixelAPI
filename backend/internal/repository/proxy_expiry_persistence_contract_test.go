package repository

import (
	"os"
	"strings"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func readNormalizedRepositorySource(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	require.NoError(t, err)
	return strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
}

func TestProxyExpirySweepPersistenceIsIdempotentOwnerSafeAndEmitsPreciseOutbox(t *testing.T) {
	source := readNormalizedRepositorySource(t, "proxy_repo.go")

	require.Contains(t, source, "func (r *proxyrepository) sweepexpiredproxies")
	require.Contains(t, source, "for update skip locked", "multiple workers must not process one proxy concurrently")
	require.Contains(t, source, "proxy_fallback_origin_id is null", "a repeated sweep must not overwrite the original binding")
	require.Contains(t, source, "proxy_id=$1", "a concurrent administrator update must make the stale sweep predicate miss")
	require.Contains(t, source, "canaccountuseproxyfallback", "fallback assignment must enforce local owner/scope/capacity policy")
	require.Contains(t, source, "returning id", "outbox payload must be based on rows actually changed")
	require.Contains(t, source, "scheduleroutboxeventaccountbulkchanged")
	require.Contains(t, source, "account_ids")
}

func TestProxyExpiryPersistenceKeepsLifecycleAndExistingLocalProxyFields(t *testing.T) {
	source := readNormalizedRepositorySource(t, "proxy_repo.go")

	// Lifecycle fields must survive both database writes and entity-to-service mapping;
	// otherwise upstream-compatible Data imports appear successful but immediately
	// disappear from subsequent reads/exports.
	for _, required := range []string{
		"setfallbackmode(proxyin.fallbackmode)",
		"setexpirywarndays(proxyin.expirywarndays)",
		"setnillableexpiresat(proxyin.expiresat)",
		"setnillablebackupproxyid(proxyin.backupproxyid)",
		"clearexpiresat()",
		"clearbackupproxyid()",
		"expiresat: m.expiresat",
		"fallbackmode: m.fallbackmode",
		"backupproxyid: m.backupproxyid",
		"expirywarndays: m.expirywarndays",
	} {
		require.Contains(t, source, required)
	}

	// The upstream lifecycle extension is additive. These local fields are already
	// user-visible contracts and must remain persisted and mapped.
	for _, required := range []string{
		"setmaxaccounts(proxyin.maxaccounts)",
		"setplatform(service.normalizeproxyplatform(proxyin.platform))",
		"setrequiredaccountlevel(service.normalizerequiredaccountlevel(proxyin.requiredaccountlevel))",
		"owneruserid: m.owneruserid",
		"platform: m.platform",
		"requiredaccountlevel: m.requiredaccountlevel",
		"maxaccounts: m.maxaccounts",
	} {
		require.Contains(t, source, required)
	}
}

func TestAccountProxyFallbackRevertRestoresOriginExactlyOnceAndInvalidatesScheduler(t *testing.T) {
	source := readNormalizedRepositorySource(t, "account_repo.go")

	require.Contains(t, source, "func (r *accountrepository) revertproxyfallback")
	require.Contains(t, source, "set proxy_id=proxy_fallback_origin_id, proxy_fallback_origin_id=null")
	require.Contains(t, source, "where id=$1 and proxy_fallback_origin_id is not null and deleted_at is null")
	require.Contains(t, source, "scheduleroutboxeventaccountchanged")
	require.Contains(t, source, "erraccountnotfound", "an account without fallback origin must fail fast")
}

func TestAccountPersistenceClearsFallbackOriginAfterExplicitProxyChoice(t *testing.T) {
	source := readNormalizedRepositorySource(t, "account_repo.go")

	require.Contains(t, source, "if account.proxyfallbackoriginid != nil")
	require.Contains(t, source, "setproxyfallbackoriginid(*account.proxyfallbackoriginid)")
	require.Contains(t, source, "clearproxyfallbackoriginid()", "a manual proxy change must make a later revert incapable of overwriting that choice")
}

func TestAccountEntityMapperPreservesProxyFallbackOrigin(t *testing.T) {
	originID := int64(77)

	mapped := accountEntityToService(&dbent.Account{ID: 4, ProxyFallbackOriginID: &originID})

	require.NotNil(t, mapped)
	require.NotNil(t, mapped.ProxyFallbackOriginID)
	require.Equal(t, originID, *mapped.ProxyFallbackOriginID)
}
