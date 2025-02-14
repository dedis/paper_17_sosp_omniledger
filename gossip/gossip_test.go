package gossip

import (
	"encoding/hex"
	"testing"
	"time"

	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"github.com/stretchr/testify/require"
)

func TestGossip1(t *testing.T) {
	log.TestOutput(true, 2)
	var nodes = 8
	Timeout = 500 * time.Millisecond
	NbNode = 3
	local := onet.NewLocalTest()
	//local := onet.NewTCPTest()
	_, _, tree := local.GenTree(nodes, true)
	defer local.CloseAll()

	pr, err := local.CreateProtocol("Gossip", tree)
	require.Nil(t, err)

	valChan := make(chan *Value, 1)
	fn := func(v *Value) {
		valChan <- v
	}
	gossip := pr.(*GossipElection)
	gossip.RegisterDoneCb(fn)
	go gossip.Start()

	select {
	case val := <-valChan:
		log.Lvlf1("val: index: %d, value: %s", val.Index, hex.EncodeToString(val.Sum))
	case <-time.After(10 * Timeout):
		t.Fatal("Test did not return...")
	}

	gossip.WaitFinish()
}

func TestGossip2(t *testing.T) {
	log.TestOutput(true, 2)
	var nodes = 8
	Timeout = 500 * time.Millisecond
	NbNode = 3
	local := onet.NewLocalTest()
	//local := onet.NewTCPTest()
	_, _, tree := local.GenTree(nodes, true)
	defer local.CloseAll()

	pr, err := local.CreateProtocol("Gossip2", tree)
	require.Nil(t, err)

	valChan := make(chan *Value)
	fn := func(v *Value) {
		valChan <- v
	}
	//gossip := pr.(*GossipElection)
	gossip := pr.(*gossip2)
	gossip.RegisterDoneCb(fn)
	go gossip.Start()

	select {
	case val := <-valChan:
		log.Lvlf1("val: index: %d, value: %s", val.Index, hex.EncodeToString(val.Sum))
	case <-time.After(10 * Timeout):
		t.Fatal("Test did not return...")
	}

	gossip.WaitFinish()
}
