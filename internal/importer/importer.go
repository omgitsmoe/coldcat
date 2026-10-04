package importer

import "time"

type File struct {
	Name               string
	PathRelativeToRoot string
	MTime              time.Time
	SizeInBytes        uint64
	HashType           HashType
	Hash               []byte
}

type FileFunc = func(file File) error
