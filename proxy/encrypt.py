"""
encrypt.py — Encrypt a real API key the same way the dashboard
does, so the proxy's DecryptRealKey can decode it back.

Matches dashboard/app/lib/crypto.ts:
  - AES-256-GCM
  - 12-byte random IV per encrypt
  - hex output: <iv 24><tag 32><ct N>
  - key derived from the same ENCRYPTION_KEY env var
"""
import os
import sys
import binascii
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


def encrypt(plaintext: str, env_key: str) -> str:
    if env_key is None or env_key == "":
        # Same dev fallback as proxy/internal/services/crypto.go
        import hashlib
        key = hashlib.sha256(b"Synapse Proxy-dev-key-do-not-use-in-prod").digest()
    else:
        raw = binascii.unhexlify(env_key)
        if len(raw) == 32:
            key = raw
        else:
            import hashlib
            key = hashlib.sha256(raw).digest()
    iv = os.urandom(12)
    aes = AESGCM(key)
    ct_with_tag = aes.encrypt(iv, plaintext.encode("utf-8"), None)
    # ct_with_tag = ciphertext || 16-byte tag
    ct, tag = ct_with_tag[:-16], ct_with_tag[-16:]
    return (iv.hex() + tag.hex() + ct.hex())


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("usage: python encrypt.py <plaintext> [ENCRYPTION_KEY]", file=sys.stderr)
        sys.exit(1)
    plaintext = sys.argv[1]
    env_key = sys.argv[2] if len(sys.argv) > 2 else os.environ.get("ENCRYPTION_KEY", "")
    print(encrypt(plaintext, env_key))