# ALC license key format

Keygate can issue license keys in the format the ALC desktop application
validates locally: `ALC-XXXX-XXXX-CCCC`.

## Enabling it

Set `ALC_KEY_PRODUCT_IDS` to the **real product IDs** (comma separated) of
the product(s) whose *new* licenses must use this format. Every other product
keeps the default `KG-XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX` format. Existing
licenses, including `KG-` ones, are never rewritten.

## Rules reproduced from ALC (`LicenseManager::validateKeyFormat` / `validateKeyChecksum`)

* exactly 18 characters, prefix `ALC-`, dashes at indexes 3, 8, 13;
* every other character is an upper-case ASCII letter or digit;
* the third group is a checksum of groups 1 and 2: `hash = 0x5A3C` (uint32);
  for each of the 8 characters `hash = hash*33 + c; hash ^= 0xB7E1`; the group
  is 4 base-34 digits (`0-9A-Z` without `I` and `O`), least significant first.

Groups 1 and 2 are drawn with `crypto/rand` from `ABCDEFGHJKLMNPQRSTUVWXYZ23456789`.
Keys are stored and looked up like any other key (SHA-256 of the trimmed,
upper-cased key, plus the optional AES-GCM copy). A new key is checked against
the stored hashes and re-drawn up to 8 times on a collision.

## Security limits (read this)

This only makes Keygate keys **accepted by the current ALC validator**. It is
not a complete licensing scheme:

1. **Reduced entropy**: 8 random characters (~40 bits) instead of the 160 bits
   of a default key.
2. **Public checksum**: the checksum function ships in the ALC binary. Anyone
   who reads it can build keys that ALC accepts locally, without Keygate.
3. **No server check in ALC**: ALC does not contact Keygate. Revocation,
   suspension, expiry and the activation counter kept by Keygate are not seen
   by the desktop app; the license shows `0 / N` activations.
4. Online activation, signed tokens, machine transfer and offline operation
   are separate work and must be validated separately.
