// Copyright 2016 Attic Labs, Inc. All rights reserved.
// Licensed under the Apache License, version 2.0:
// http://www.apache.org/licenses/LICENSE-2.0

package nbs

import (
	"bytes"
	"testing"

	"github.com/attic-labs/testify/assert"
)

type memReaderAt []byte

func (mr memReaderAt) ReadAt(buff []byte, offset int64) (n int, err error) {
	copy(buff, mr[offset:])
	n = len(buff)
	if int64(len(mr))-offset < int64(n) {
		n = int(int64(len(mr)) - offset)
	}
	return
}

func TestMemTableAddHasGetChunk(t *testing.T) {
	assert := assert.New(t)
	mt := newMemTable(1024)

	chunks := [][]byte{
		[]byte("hello2"),
		[]byte("goodbye2"),
		[]byte("badbye2"),
	}

	for _, c := range chunks {
		assert.True(mt.addChunk(computeAddr(c), c))
	}

	assertChunksInReader(chunks, mt, assert)

	for _, c := range chunks {
		assert.Equal(bytes.Compare(c, mt.get(computeAddr(c))), 0)
	}

	notPresent := []byte("nope")
	assert.False(mt.has(computeAddr(notPresent)))
	assert.Nil(mt.get(computeAddr(notPresent)))
}

func TestMemTableAddOverflowChunk(t *testing.T) {
	memTableSize := uint64(1024)

	assert := assert.New(t)
	big := make([]byte, memTableSize)
	little := []byte{0x01}
	{
		bigAddr := computeAddr(big)
		mt := newMemTable(memTableSize)
		assert.True(mt.addChunk(bigAddr, big))
		assert.True(mt.has(bigAddr))
		assert.False(mt.addChunk(computeAddr(little), little))
		assert.False(mt.has(computeAddr(little)))
	}

	{
		big := big[:memTableSize-1]
		bigAddr := computeAddr(big)
		mt := newMemTable(memTableSize)
		assert.True(mt.addChunk(bigAddr, big))
		assert.True(mt.has(bigAddr))
		assert.True(mt.addChunk(computeAddr(little), little))
		assert.True(mt.has(computeAddr(little)))
		other := []byte("o")
		assert.False(mt.addChunk(computeAddr(other), other))
		assert.False(mt.has(computeAddr(other)))
	}
}

func TestMemTableWrite(t *testing.T) {
	assert := assert.New(t)
	mt := newMemTable(1024)

	chunks := [][]byte{
		[]byte("hello2"),
		[]byte("goodbye2"),
		[]byte("badbye2"),
	}

	for _, c := range chunks {
		assert.True(mt.addChunk(computeAddr(c), c))
	}

	td1, _ := buildTable(chunks[1:2])
	td2, _ := buildTable(chunks[2:])
	tr1, tr2 := newTableReader(td1, memReaderAt(td1)), newTableReader(td2, memReaderAt(td2))
	assert.True(tr1.has(computeAddr(chunks[1])))
	assert.True(tr2.has(computeAddr(chunks[2])))

	writeSize := maxTableSize(1, uint64(len(chunks[0])))

	buff := make([]byte, writeSize)
	tw := newTableWriter(buff)

	mt.write(tw, chunkReaderGroup{tr1, tr2})

	ll, _ := tw.finish()

	outReader := newTableReader(buff[:ll], memReaderAt(buff[:ll]))
	assert.True(outReader.has(computeAddr(chunks[0])))
	assert.False(outReader.has(computeAddr(chunks[1])))
	assert.False(outReader.has(computeAddr(chunks[2])))
}

type chunkReaderGroup []chunkReader

func (crg chunkReaderGroup) has(h addr) bool {
	for _, haver := range crg {
		if haver.has(h) {
			return true
		}
	}
	return false
}

func (crg chunkReaderGroup) get(h addr) []byte {
	for _, haver := range crg {
		if data := haver.get(h); data != nil {
			return data
		}
	}
	return nil
}

func (crg chunkReaderGroup) hasMany(addrs []hasRecord) (remaining bool) {
	for _, haver := range crg {
		if !haver.hasMany(addrs) {
			return false
		}
	}

	return true
}

func (crg chunkReaderGroup) getMany(reqs []getRecord) (remaining bool) {
	for _, haver := range crg {
		if !haver.getMany(reqs) {
			return false
		}
	}

	return true
}