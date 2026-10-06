package base

import (
	"crypto"
	"fmt"
)

type HashType struct {
	crypto.Hash
}

func (h HashType) ToIdentifier() (string, error) {
	switch h.Hash {
	case crypto.MD4:
		return "md4", nil
	case crypto.MD5:
		return "md5", nil
	case crypto.SHA1:
		return "sha1", nil
	case crypto.SHA256:
		return "sha256", nil
	case crypto.SHA384:
		return "sha384", nil
	case crypto.SHA3_224:
		return "sha3_224", nil
	case crypto.SHA3_256:
		return "sha3_256", nil
	case crypto.SHA3_384:
		return "sha3_384", nil
	case crypto.SHA3_512:
		return "sha3_512", nil
	case crypto.SHA512:
		return "sha512", nil

	case crypto.SHA224:
		fallthrough
	case crypto.SHA512_224:
		fallthrough
	case crypto.SHA512_256:
		fallthrough
	case crypto.BLAKE2b_256:
		fallthrough
	case crypto.BLAKE2b_384:
		fallthrough
	case crypto.BLAKE2b_512:
		fallthrough
	case crypto.BLAKE2s_256:
		fallthrough
	case crypto.MD5SHA1:
		fallthrough
	case crypto.RIPEMD160:
		return "", fmt.Errorf("unsupported hash %v", h.Hash.String())
	}

	return "", fmt.Errorf("unsupported hash identifier: %d", h.Hash)
}

var identifierToHash = map[string]HashType{
	"md4":      {Hash: crypto.MD4},
	"md5":      {Hash: crypto.MD5},
	"sha1":     {Hash: crypto.SHA1},
	"sha256":   {Hash: crypto.SHA256},
	"sha384":   {Hash: crypto.SHA384},
	"sha3_224": {Hash: crypto.SHA3_224},
	"sha3_256": {Hash: crypto.SHA3_256},
	"sha3_384": {Hash: crypto.SHA3_384},
	"sha3_512": {Hash: crypto.SHA3_512},
	"sha512":   {Hash: crypto.SHA512},
}

func FromIdentifier(id string) (HashType, error) {
	if h, ok := identifierToHash[id]; ok {
		return h, nil
	}

	return HashType{}, fmt.Errorf("unknown hash identifier: %q", id)
}
