// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkeddb

// LinkedDB is deprecated, with the implementation here violating the original
// design considerations for a significant performance boost, while still
// maintaining reverse compatibility to how it was actually used in production.

import (
	"github.com/ava-labs/avalanchego/database"
)

var (
	headKey       = []byte{0x01} // deprecated
	nodeKeyPrefix = byte(0x00)

	_ LinkedDB          = (*linkedDB)(nil)
	_ database.Iterator = (*iterator)(nil)
)

// LinkedDB provides a key value interface while allowing iteration.
type LinkedDB interface {
	database.KeyValueReaderWriterDeleter

	IsEmpty() (bool, error)
	HeadKey() ([]byte, error)
	Head() (key []byte, value []byte, err error)

	NewIterator() database.Iterator
	NewIteratorWithStart(start []byte) database.Iterator
}

type linkedDB struct {
	// db is the underlying database that this list is stored in.
	db database.Database
}

type node struct {
	Value       []byte `serialize:"true"`
	HasNext     bool   `serialize:"true"`
	Next        []byte `serialize:"true"`
	HasPrevious bool   `serialize:"true"`
	Previous    []byte `serialize:"true"`
}

func encodeValue(value []byte) []byte {
	n := node{Value: value}
	nodeBytes, err := Codec.Marshal(CodecVersion, n)
	if err != nil {
		panic(err)
	}
	return nodeBytes
}

func decodeValue(nodeBytes []byte) []byte {
	var n node
	if _, err := Codec.Unmarshal(nodeBytes, &n); err != nil {
		panic(err)
	}
	return n.Value
}

type iterator struct {
	database.Iterator
}

// Key implements database.Iterator.
func (i *iterator) Key() []byte {
	k := i.Iterator.Key()
	if len(k) == 0 || k[0] != nodeKeyPrefix {
		return nil
	}
	return k[1:]
}

func (i *iterator) Next() bool {
	ok := i.Iterator.Next()
	if !ok {
		return false
	}
	k := i.Iterator.Key()
	// if we iterated all the way to the legacy "head" node which has a prefix
	// byte of 1, we are done iterating
	if len(k) == 0 || k[0] != nodeKeyPrefix {
		return false
	}
	return true
}

func (i *iterator) Value() []byte {
	return decodeValue(i.Iterator.Value())
}

func nodeKey(key []byte) []byte {
	newKey := make([]byte, len(key)+1)
	copy(newKey[1:], key)
	return newKey
}

func New(db database.Database) LinkedDB {
	return &linkedDB{
		db: db,
	}
}

func NewDefault(db database.Database) LinkedDB {
	return New(db)
}

func (ldb *linkedDB) Has(key []byte) (bool, error) {
	return ldb.db.Has(nodeKey(key))
}

func (ldb *linkedDB) Get(key []byte) ([]byte, error) {
	v, err := ldb.db.Get(nodeKey(key))
	if err != nil {
		return nil, err
	}
	return decodeValue(v), nil
}

func (ldb *linkedDB) Put(key, value []byte) error {
	return ldb.db.Put(nodeKey(key), encodeValue(value))
}

func (ldb *linkedDB) Delete(key []byte) error {
	return ldb.db.Delete(nodeKey(key))
}

func (ldb *linkedDB) IsEmpty() (bool, error) {
	_, err := ldb.HeadKey()
	if err == database.ErrNotFound {
		return true, nil
	}
	return false, err
}

func (ldb *linkedDB) HeadKey() ([]byte, error) {
	k, _, err := ldb.Head()
	return k, err
}

func (ldb *linkedDB) Head() ([]byte, []byte, error) {
	iter := ldb.db.NewIterator()
	defer iter.Release()
	if !iter.Next() {
		return nil, nil, database.ErrNotFound
	}

	k := iter.Key()
	if len(k) == 0 || k[0] != nodeKeyPrefix {
		return nil, nil, database.ErrNotFound
	}
	return k[1:], decodeValue(iter.Value()), nil
}

func (ldb *linkedDB) NewIterator() database.Iterator {
	return &iterator{ldb.db.NewIterator()}
}

// NewIteratorWithStart returns an iterator that starts at [start].
func (ldb *linkedDB) NewIteratorWithStart(start []byte) database.Iterator {
	return &iterator{ldb.db.NewIteratorWithStart(start)}
}
