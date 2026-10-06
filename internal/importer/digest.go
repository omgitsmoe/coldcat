package importer

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"

	"github.com/omgitsmoe/coldcat/internal/database"
)

type inventoryDigest struct {
	h hash.Hash
}

func newInventoryDigest() inventoryDigest {
	d := inventoryDigest{h: sha256.New()}
	d.bytes([]byte("coldcat-semantic-inventory-v1"))
	return d
}

func (d inventoryDigest) number(n uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], n)
	d.h.Write(encoded[:])
}

func (d inventoryDigest) bytes(value []byte) {
	d.number(uint64(len(value)))
	d.h.Write(value)
}

func (d inventoryDigest) add(file File) error {
	algorithm, err := file.HashType.ToIdentifier()
	if err != nil {
		return err
	}
	d.bytes([]byte(file.path()))
	d.bytes([]byte(algorithm))
	d.bytes(file.Hash)
	if file.SizeKnown {
		d.number(1)
		d.number(file.SizeInBytes)
	} else {
		d.number(0)
	}
	if file.MTimeKnown {
		d.number(1)
		d.bytes([]byte(database.FormatTime(file.MTime)))
	} else {
		d.number(0)
	}
	return nil
}

func (d inventoryDigest) sum() [sha256.Size]byte {
	return [sha256.Size]byte(d.h.Sum(nil))
}
