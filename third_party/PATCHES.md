# third_party

## golang.org/x/crypto

Copy of `github.com/ys-ll/crypto v0.57.1-0.20260922023551-18e0be45963e`
(go.sum `h1:Q3sa/5WcZnIiEcjy+3ex/p+Y+fQpN4DNnm+QwJjdYYQ=`), the x/crypto fork
the app already used (it adds aes192-cbc/aes256-cbc for old devices), now
kept in the repository so the build no longer depends on the upstream
maintainer's fork. Tests outside `ssh/` were dropped to keep the copy small.

Local patch (search for "FlexTerm patch"):

- `ssh/client.go`, `ssh/handshake.go`: `ClientConfig.HostKeySignatureVerifier`,
  an optional per-connection replacement for `hostKey.Verify` when checking
  the server's signature over the exchange hash. The app uses it only on
  connections marked "Legacy device" to accept RSA host keys below the
  1024-bit minimum crypto/rsa enforces (see
  `backend/session/ssh_legacy.go`), without relaxing that minimum for the
  whole process (`GODEBUG=rsa1024min=0`).

Updating: copy the new x/crypto (or fork) release over this directory,
re-apply the patch above, run `go test ./ssh/` here and
`go test ./backend/session/` in the app.
