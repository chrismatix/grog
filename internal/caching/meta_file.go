package caching

import "encoding/json"

// MetaFile is a wrapper format for .meta files which we store along side the cache files
// to record the hash of the file that was cached (faster than hashing the file again)
type MetaFile struct {
	Hash string `json:"hash"`
}

func NewMetaFile(hash string) *MetaFile {
	return &MetaFile{
		Hash: hash,
	}
}

func (m *MetaFile) GetHash() string {
	return m.Hash
}

func (m *MetaFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(m)
}
