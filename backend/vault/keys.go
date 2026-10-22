package vault

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/hpke"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

// vaultKeySize: the vault key (VK) of an epoch is 256 random bits. There is
// no password to derive it from, so a copy of the folder has nothing to
// brute-force.
const vaultKeySize = 32

func newVaultKey() ([]byte, error) {
	vk := make([]byte, vaultKeySize)
	_, err := rand.Read(vk)
	return vk, err
}

// keyCheck commits to an epoch's key in the signed roster. Only a holder of
// the key can compute it, and it reveals nothing about it (HMAC under the
// key itself). It makes HPKE's unauthenticated sender safe: a wrap planted
// by anyone who can write to storage carries a key that fails this check.
func keyCheck(vaultID string, epoch uint64, vk []byte) string {
	m := hmac.New(sha256.New, vk)
	m.Write([]byte("uniterm-vault/v1 keycheck\x00"))
	writeField(m, []byte(vaultID))
	writeField(m, []byte(strconv.FormatUint(epoch, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

func wrapName(deviceID string, epoch uint64) string {
	return fmt.Sprintf("keys/%s.%d", deviceID, epoch)
}

// hpkeInfo binds a wrap to its vault and epoch (design: "uniterm-vault|
// <vaultID>|<epoch>").
func hpkeInfo(vaultID string, epoch uint64) []byte {
	return []byte("uniterm-vault|" + vaultID + "|" + strconv.FormatUint(epoch, 10))
}

// HPKE suite: DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-256-GCM (RFC 9180
// base mode; the sender is authenticated by keyCheck, see above).
func wrapKey(kexPub []byte, vaultID string, epoch uint64, vk []byte) ([]byte, error) {
	pub, err := ecdh.P256().NewPublicKey(kexPub)
	if err != nil {
		return nil, err
	}
	pk, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return hpke.Seal(pk, hpke.HKDFSHA256(), hpke.AES256GCM(), hpkeInfo(vaultID, epoch), vk)
}

var errBadWrap = errors.New("vault: wrapped key does not match the roster")

// unwrapKey opens this device's wrap of an epoch key and checks it against
// the roster's commitment.
func unwrapKey(dev DeviceKey, r *Roster, epoch uint64, wrapped []byte) ([]byte, error) {
	if epoch == 0 || epoch > uint64(len(r.KeyChecks)) {
		return nil, fmt.Errorf("vault: no epoch %d in the roster", epoch)
	}
	sk, err := hpke.NewDHKEMPrivateKey(dev.KeyExchanger())
	if err != nil {
		return nil, err
	}
	vk, err := hpke.Open(sk, hpke.HKDFSHA256(), hpke.AES256GCM(), hpkeInfo(r.VaultID, epoch), wrapped)
	if err != nil {
		return nil, fmt.Errorf("vault: unwrap epoch %d: %w", epoch, err)
	}
	if len(vk) != vaultKeySize || !hmac.Equal([]byte(keyCheck(r.VaultID, epoch, vk)), []byte(r.KeyChecks[epoch-1])) {
		return nil, errBadWrap
	}
	return vk, nil
}
