package utxos

import (
	"encoding/base64"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// extractAVMCredentials extracts credentials (publicKey, signature) from an AVM tx.
func extractAVMCredentials(tx *txs.Tx) []Credential {
	if tx == nil || len(tx.Creds) == 0 {
		return nil
	}

	unsignedBytes := tx.Unsigned.Bytes()
	var creds []Credential
	seen := make(map[string]bool) // dedupe by signature

	for _, fxCred := range tx.Creds {
		secpCred, ok := fxCred.Credential.(*secp256k1fx.Credential)
		if !ok {
			continue
		}

		for _, sig := range secpCred.Sigs {
			// Recover public key from signature
			pubKey, err := secp256k1.RecoverPublicKey(unsignedBytes, sig[:])
			if err != nil {
				continue
			}

			sigB64 := base64.RawStdEncoding.EncodeToString(sig[:])
			if seen[sigB64] {
				continue // skip duplicate
			}
			seen[sigB64] = true

			creds = append(creds, Credential{
				PublicKey: base64.RawStdEncoding.EncodeToString(pubKey.Bytes()),
				Signature: sigB64,
			})
		}
	}
	return creds
}

// extractCChainCredentials extracts credentials (publicKey, signature) from an atomic tx.
// Renamed from extractCredentials to avoid confusion.
func extractCChainCredentials(tx *atomicTx) []Credential {
	if tx == nil || len(tx.Creds) == 0 {
		return nil
	}

	// Marshal the unsigned tx to get the message that was signed
	unsignedBytes, err := atomicCodec.Marshal(atomicCodecVersion, tx.UnsignedAtomicTx)
	if err != nil {
		return nil
	}

	var creds []Credential
	seen := make(map[string]bool) // dedupe by signature
	for _, cred := range tx.Creds {
		secpCred, ok := cred.(*secp256k1fx.Credential)
		if !ok {
			continue
		}

		for _, sig := range secpCred.Sigs {
			// Recover public key from signature
			pubKey, err := secp256k1.RecoverPublicKey(unsignedBytes, sig[:])
			if err != nil {
				continue
			}

			sigB64 := base64.RawStdEncoding.EncodeToString(sig[:])
			if seen[sigB64] {
				continue // skip duplicate
			}
			seen[sigB64] = true

			creds = append(creds, Credential{
				PublicKey: base64.RawStdEncoding.EncodeToString(pubKey.Bytes()),
				Signature: sigB64,
			})
		}
	}
	return creds
}
