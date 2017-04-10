package gossip

import (
	"bytes"
	"errors"
	"math/rand"
	"time"

	"github.com/dedis/onet"
	"github.com/dedis/onet/log"
)

var Msg = []byte("hellothisisthecustomstringthatiscompletelypubliclyavailable")

// how much time do we sleep
var Timeout time.Duration = 2 // in seconds
// how much time do we run the "gossip" part
var NbGossips = 5

// to how much node do we send the "gossip"
var NbNode = 5

type GossipElection struct {
	*onet.TreeNodeInstance
	incomingValue chan Value
	stop          chan bool
	doneCb        func(v *Value)
	value         *Value
}

type Value struct {
	Index uint32     // index of the issuer of the value
	Sum   []byte     // the value itself
	Proof *DLEQProof // the proof
}

type ValueFound Value

type ValueHeap []*Value

func NewGossipElection(n *onet.TreeNodeInstance) *GossipElection {
	ge := &GossipElection{
		TreeNodeInstance: n,
		incomingValue:    make(chan Value),
		stop:             make(chan bool),
	}
	ge.genValue()
	log.ErrFatal(ge.RegisterHandler(ge.ReceiveValue))
	log.ErrFatal(ge.RegisterHandler(ge.ReceiveValueFound))
	return ge
}

func (p *GossipElection) Dispatch() error {
	lowest := p.value
	threshold := p.Tree().Size() * 2.0 / 3.0
	rootValues := newValueCounter(threshold)
	for {
		select {
		case v := <-p.incomingValue:
			if p.IsRoot() {
				rootValues.Put(&v)
				if rootValues.ThresholdReached() {
					// found !
					log.Lvl2(p.Name(), "Received ", rootValues.Max().Counter, "values ! Stopping!")
					p.foundAll(rootValues.Max().Value)
					return nil
				}
			}
			if lowest.Less(&v) {
				continue
			}
			log.Lvl3(p.Name(), "found lower value", string(v.Sum))
			lowest = &v
		case <-time.After(time.Second * Timeout):
			log.Lvl4(p.Name(), "timeout occured => gossiping")
			p.gossip(lowest)
			if p.IsRoot() {
				rootValues.Reset()
			}
		case <-p.stop:
			log.Lvl3(p.Name(), "stopping...")
			return nil
		}
	}
}

func (p *GossipElection) ReceiveValue(n *onet.TreeNode, val *Value) error {
	if !Verify(p.Suite(), n.ServerIdentity.Public, Msg, val.Sum, val.Proof) {
		log.Error(p.Name(), "received invalid proof from", n.Name())
		return errors.New("invalid proof")
	}
	p.incomingValue <- *val
	return nil
}

func (p *GossipElection) ReceiveValueFound(n *onet.TreeNode, val *ValueFound) error {
	log.Lvl2(p.Name(), "received found value from root ! Stopping..")
	if err := p.SendToChildren(val); err != nil {
		log.Error(err)
	}
	close(p.stop)
	p.Done()
	return nil
}

func (p *GossipElection) foundAll(common *Value) {
	if p.doneCb != nil {
		p.doneCb(common)
	}
	vf := ValueFound(*common)
	p.ReceiveValueFound(p.TreeNode(), &vf)
}

func (p *GossipElection) RegisterDoneCb(fn func(v *Value)) {
	p.doneCb = fn
}

func (p *GossipElection) gossip(lowest *Value) {
	// send to root
	p.SendTo(p.Root(), lowest)
	// choose random nodes
	list := p.List()
	for i := 0; i < NbNode; i++ {
		n := rand.Intn(len(list))
		if err := p.SendTo(list[n], lowest); err != nil {
			log.Error(p.Name(), "err gossiping to", list[n].Name())
		}
	}

}

func (p *GossipElection) genValue() *Value {
	idx := uint32(p.Index())
	sum, proof, err := Hash(p.Suite(), p.Private(), Msg)
	log.ErrFatal(err)
	p.value = &Value{idx, sum, proof}
	return p.value
}

func (v *Value) Less(v2 *Value) bool {
	iv := v.Sum
	jv := v2.Sum
	return bytes.Compare(iv, jv) < 0

}

func (vh *ValueHeap) Len() int {
	return len(*vh)
}

func (vh *ValueHeap) Less(i, j int) bool {
	iv := (*vh)[i].Sum
	jv := (*vh)[j].Sum
	return bytes.Compare(iv, jv) < 0
}

func (vh *ValueHeap) Swap(i, j int) {
	tmp := (*vh)[i]
	(*vh)[i] = (*vh)[j]
	(*vh)[j] = tmp
}

func (vh *ValueHeap) Push(x interface{}) {
	v := x.(*Value)
	(*vh) = append(*vh, v)
}

func (vh *ValueHeap) Pop() interface{} {
	old := *vh
	n := len(old)
	x := old[n-1]
	*vh = old[0 : n-1]
	return x
}

type valueInfo struct {
	*Value
	Counter int
}

type valueCounter struct {
	m         map[string]valueInfo
	max       valueInfo
	threshold int
}

func newValueCounter(threshold int) *valueCounter {
	return &valueCounter{
		m:         make(map[string]valueInfo),
		threshold: threshold,
	}
}

func (v *valueCounter) ThresholdReached() bool {
	if v.max.Counter > v.threshold {
		return true
	}
	return false
}

func (v *valueCounter) Max() valueInfo {
	return v.max
}

func (v *valueCounter) Put(val *Value) {
	str := string(val.Sum)
	vi, ok := v.m[str]
	if !ok {
		vi = valueInfo{val, 0}
	}
	vi.Counter++
	v.m[str] = vi
	if vi.Counter > v.max.Counter {
		v.max = vi
	}
}

func (v *valueCounter) Reset() {
	v.m = make(map[string]valueInfo)
	v.max = valueInfo{}
}
