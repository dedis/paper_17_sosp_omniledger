package gossip

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/dedis/onet"
	"github.com/dedis/onet/log"
)

func init() {
	onet.GlobalProtocolRegister("Gossip2", NewGossipElection2)
}

type gossip2 struct {
	*onet.TreeNodeInstance
	value     *Value
	threshold int

	rootValues *valueCounter
	rootMut    sync.Mutex

	lowest    *Value
	lowestMut sync.Mutex

	finishedChildren int
	finishedMut      sync.Mutex

	foundValue *ValueFound
	foundMut   sync.Mutex

	rootDone    bool
	rootDoneMut sync.Mutex

	stop   chan bool
	waitCh chan bool
	doneCb func(*Value)
}

func NewGossipElection2(n *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {
	idx := uint32(n.Index())
	sum, proof, err := Hash(n.Suite(), n.Private(), Msg)
	log.ErrFatal(err)
	value := &Value{idx, sum, proof}
	threshold := n.Tree().Size() * 2.0 / 3.0
	g := &gossip2{
		TreeNodeInstance: n,
		value:            value,
		rootValues:       newValueCounter(threshold),
		threshold:        threshold,
		lowest:           value,
		stop:             make(chan bool, 1),
		waitCh:           make(chan bool, 1),
	}
	go g.timer()
	log.ErrFatal(g.RegisterHandler(g.ReceiveValue))
	log.ErrFatal(g.RegisterHandler(g.ReceiveValueFound))
	log.ErrFatal(g.RegisterHandler(g.ReceiveFinished))

	return g, nil
}

func (g *gossip2) Start() error {
	log.Print("Root gossiping...")
	g.gossip()
	return nil
}

func (g *gossip2) timer() {
	for {
		select {
		case <-time.After(Timeout):
			g.gossip()
		case <-g.stop:
			return
		}
	}
}

func (g *gossip2) gossip() {
	g.lowestMut.Lock()
	defer g.lowestMut.Unlock()
	// choose random nodes

	list := g.List()
	taken := make(map[int]bool)
	for i := 0; i < NbNode; i++ {
		var n = rand.Intn(len(list))
		for taken[n] && n != g.Index() {
			n = rand.Intn(len(list))
		}
		taken[n] = true
		//log.Print(p.Name(), "sending to", list[n].Name(), " for idx ", p.lowest.Index)
		if err := g.SendTo(list[n], g.lowest); err != nil {
			log.Error(g.Name(), "err gossiping to", list[n].Name(), err)
		}
	}

	// send to root
	g.SendTo(g.Root(), g.lowest)
}

func (g *gossip2) ReceiveValue(v ValueHandler) error {
	val := &v.Value
	n := v.TreeNode
	public := g.Roster().List[int(val.Index)].Public
	if !Verify(g.Suite(), public, Msg, val.Sum, val.Proof) {
		log.Error(g.Name(), "received invalid proof from", n.Name())
		return errors.New("invalid proof")
	}

	log.Lvl3(g.Name(), "received value ", v.Value.Index, "from", v.TreeNode.Name())

	g.lowestMut.Lock()
	if !g.lowest.Less(val) {
		g.lowest = val
	}
	g.lowestMut.Unlock()

	if g.IsRoot() {
		g.rootMut.Lock()
		g.rootValues.Put(val)
		if g.rootValues.ThresholdReached() {
			g.rootDoneMut.Lock()
			if g.rootDone {
				g.rootDoneMut.Unlock()
				return nil
			}
			g.rootDone = true
			g.rootDoneMut.Unlock()
			// gossiping done !
			maxVal := g.rootValues.Max()
			log.Lvlf2("root has %d/%d values for idx %d", maxVal.Counter, g.threshold, maxVal.Index)
			// closing timer
			close(g.stop)
			// sending the value to children
			found := ValueFound(*maxVal.Value)
			if err := g.SendToChildren(&found); err != nil {
				log.Error(err)
			}
			// callback
			if g.doneCb != nil {
				go g.doneCb(maxVal.Value)
			}
			// setting the right value
			g.foundMut.Lock()
			g.foundValue = &found
			g.foundMut.Unlock()
		}
		g.rootMut.Unlock()
	}
	return nil
}

func (g *gossip2) ReceiveValueFound(v ValueFoundHandler) error {
	val := &v.ValueFound
	log.Lvl2(g.Name(), "received from root found value", val.Index, "! Sending finishing..")
	// closing timer
	close(g.stop)

	// checking with our value
	g.lowestMut.Lock()
	if g.lowest.Index != val.Index {
		log.Lvl2(g.Name(), "have a different idx mine(", g.lowest.Index, ") vs root(", val.Index, ")")
	}
	g.lowestMut.Unlock()

	g.foundMut.Lock()
	g.foundValue = val
	g.foundMut.Unlock()

	// sending the value down the tree
	if err := g.SendToChildren(val); err != nil {
		log.Error(err)
	}

	// if leaf, then send up finished
	if g.IsLeaf() {
		if err := g.SendToParent(&Finished{}); err != nil {
			log.Error(err)
		}
	}
	return nil
}

func (g *gossip2) ReceiveFinished(f FinishedHandler) error {
	log.Print(g.Name(), "received FINISHED message from", f.Name())

	g.finishedMut.Lock()
	defer g.finishedMut.Unlock()
	g.finishedChildren++
	if g.finishedChildren < len(g.Children()) {
		if g.IsRoot() {
			log.Print("Root did not receive all its children.. !!!!!!!!!!!!!!")
		}
		return nil
	}

	if err := g.SendToParent(&Finished{}); err != nil {
		log.Error(err)
	}

	if !g.IsRoot() {
		log.Lvl2(g.Name(), "node finished & send to", g.Parent().Name())
	}

	if g.IsRoot() {
		log.Lvl2(g.Name(), "root received all finished -> closing")
		close(g.waitCh)
	}
	g.Done()
	return nil
}

func (g *gossip2) WaitFinish() {
	<-g.waitCh
}

func (g *gossip2) RegisterDoneCb(fn func(*Value)) {
	g.doneCb = fn
}
