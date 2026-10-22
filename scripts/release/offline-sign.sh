#!/bin/sh
# Signs a release's checksums.txt. RUN ONLY ON THE OFFLINE MACHINE.
#
#   sh offline-sign.sh <uniterm-release.key> <checksums.txt>
#
# Shows the file for review, asks for confirmation, writes
# <checksums.txt>.sig (64 raw bytes) and verifies it. Only checksums.txt and
# checksums.txt.sig cross between machines — never the key.
# Set UNITERM_RELEASE_KEY_PASS to supply the passphrase non-interactively.
set -eu

key=${1:?usage: offline-sign.sh <key> <checksums.txt>}
sums=${2:?usage: offline-sign.sh <key> <checksums.txt>}
sig="$sums.sig"

case "$(openssl version)" in
  "OpenSSL 1."*|LibreSSL*) echo "need OpenSSL >= 3; got: $(openssl version)" >&2; exit 1 ;;
esac

# Every line must be "<64 hex>  uniterm-<name>"; anything else is refused.
if grep -vqE '^[0-9a-f]{64}  uniterm-[A-Za-z0-9._+-]+$' "$sums"; then
  echo "refusing: $sums has lines that are not '<sha256>  uniterm-<file>'" >&2
  exit 1
fi

echo "About to sign:"
echo "----------------------------------------"
cat "$sums"
echo "----------------------------------------"
printf 'Sign these %s files? [y/N] ' "$(wc -l < "$sums" | tr -d ' ')"
read -r answer
case "$answer" in y|Y|s|S) ;; *) echo "aborted"; exit 1 ;; esac

if [ -n "${UNITERM_RELEASE_KEY_PASS:-}" ]; then
  passin="-passin env:UNITERM_RELEASE_KEY_PASS"
else
  passin=""
fi
# shellcheck disable=SC2086
openssl pkeyutl -sign -rawin -inkey "$key" $passin -in "$sums" -out "$sig"
# shellcheck disable=SC2086
openssl pkey -in "$key" $passin -pubout -out "$sig.pub.pem.tmp"
if openssl pkeyutl -verify -rawin -pubin -inkey "$sig.pub.pem.tmp" -in "$sums" -sigfile "$sig" >/dev/null; then
  rm -f "$sig.pub.pem.tmp"
  echo "Signed: $sig (copy it next to checksums.txt on the build machine)"
else
  rm -f "$sig.pub.pem.tmp" "$sig"
  echo "signature self-check failed" >&2
  exit 1
fi
