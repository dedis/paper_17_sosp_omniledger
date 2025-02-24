package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/dedis/onet.v1/log"
)

func TestReadBlocks(t *testing.T) {
	ppv := 16
	block, err := readCSV("blocks-size.csv", ppv, true)
	log.ErrFatal(err)
	last := block.getIndex(0)
	//for i := 8920; i < 8940; i++ {
	for i := range block.values[1:] {
		next := block.getIndex(i + 1)
		if last > 0 || next > 0 {
			assert.True(t, next >= last,
				"%d: %d is not bigger than %d. Next: %d",
				i+1, next, last, block.values[i+1])
		}
		last = next
	}
	val := block.GetValue(0)
	for n := 0; n < (len(block.values)-1)*ppv; n++ {
		//for n := 48127; n < 48131; n++ {
		next := block.GetValue(n)
		assert.True(t, next >= val, "%d: %d not bigger than %d",
			n, next, val)
		val = next
	}

	unspent, err := readCSV("utxo-count.csv", ppv, false)
	log.ErrFatal(err)
	for i := range unspent.values {
		assert.True(t, unspent.getIndex(i) >= 0)
		assert.True(t, unspent.GetValue(i) >= 0)
	}
	assert.True(t, unspent.first > 0)
}
