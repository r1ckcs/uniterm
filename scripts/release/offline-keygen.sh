#!/bin/sh
# Generates the uniTerm release signing key. RUN ONLY ON THE OFFLINE MACHINE
# (clean computer or live USB, network off) — see RELEASES.md. The private
# key must never touch a networked or previously compromised machine.
#
#   sh offline-keygen.sh <output-dir>
#
# Writes <output-dir>/uniterm-release.key (PEM, AES-256 encrypted with a
# passphrase you type, mode 0600) and <output-dir>/release-pubkey.txt
# (base64 of the 32-byte public key: the only file that leaves this machine).
# Set UNITERM_RELEASE_KEY_PASS to supply the passphrase non-interactively.
set -eu

out=${1:?usage: offline-keygen.sh <output-dir>}
key="$out/uniterm-release.key"
pub="$out/release-pubkey.txt"

command -v openssl >/dev/null || { echo "openssl not found" >&2; exit 1; }
case "$(openssl version)" in
  "OpenSSL 1."*|LibreSSL*) echo "need OpenSSL >= 3 (ed25519 with -rawin); got: $(openssl version)" >&2; exit 1 ;;
esac
[ -e "$key" ] && { echo "refusing to overwrite $key" >&2; exit 1; }
mkdir -p "$out"

umask 077
if [ -n "${UNITERM_RELEASE_KEY_PASS:-}" ]; then
  openssl genpkey -algorithm ed25519 -aes-256-cbc -pass env:UNITERM_RELEASE_KEY_PASS -out "$key"
  passin="-passin env:UNITERM_RELEASE_KEY_PASS"
else
  echo "Choose a strong passphrase for the private key (you will need it for every release):"
  openssl genpkey -algorithm ed25519 -aes-256-cbc -out "$key"
  passin=""
fi
chmod 600 "$key"

# The DER SubjectPublicKeyInfo of an ed25519 key ends with the 32 raw bytes.
# shellcheck disable=SC2086
openssl pkey -in "$key" $passin -pubout -outform DER | tail -c 32 | openssl base64 -A > "$pub"
echo >> "$pub"
chmod 644 "$pub"

fp=$(openssl base64 -d -A < "$pub" | openssl dgst -sha256 -r | cut -c1-32 | sed 's/..../& /g; s/ $//')
echo
echo "Private key : $key   (keep offline; back it up on a second offline medium)"
echo "Public key  : $pub   (copy this file to the repo as build/release-pubkey.txt)"
echo "Fingerprint : $fp"
echo "Write the fingerprint down: sigtool fingerprint must print the same on the build machine."
