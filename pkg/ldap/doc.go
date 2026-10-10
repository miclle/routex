// Package ldap authenticates a rechecked directory attribute through a fresh,
// guarded LDAPS service bind, exact-one subtree search, user bind, and exact-DN
// base-object reread. It does not establish an application binding or follow
// referrals. The selected entryUUID/objectGUID bytes are opaque; DN is transient.
//
// The network operation has one parent-shortened ten-second deadline. Cleanup has
// a separate one-second observation allowance, without another LDAP operation.
// Raw network I/O is closed and our cancellation callback is joined before return.
// The maintained go-ldap client is closed synchronously. Its public Close method
// does not expose a join of its private reader and timeout workers; this package
// does not claim such a join. No shared connection or request goroutine is exposed.
package ldap
