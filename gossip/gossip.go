package gossip

import (
	"bytes"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/dedis/onet"
	"github.com/dedis/onet/log"
	"github.com/dedis/onet/network"
)

func init() {
	onet.GlobalProtocolRegister("Gossip", NewGossipElection)
	network.RegisterMessage(&Value{})
	network.RegisterMessage(&ValueFound{})
	network.RegisterMessage(&Finished{})
}

var Msg = []byte("hellothisisthecustomstringthatiscompletelypubliclyavailable")

// how much time do we sleep
var Timeout = 2 * time.Second

// to how much node do we send the "gossip"
var NbNode = 5

type GossipElection struct {
	*onet.TreeNodeInstance
	threshold    int
	stop         chan bool
	waitCh       chan bool
	done         bool
	doneCb       func(v *Value)
	value        *Value
	newValue     chan *Value
	valueFoundCh chan *ValueFound
	newRootValue chan *Value
	firstTime    bool
	list         *onet.Roster
	finished     int
	sync.Mutex
}

type Value struct {
	Index uint32     // index of the issuer of the value
	Sum   []byte     // the value itself
	Proof *DLEQProof // the proof
}

type ValueFound Value

type ValueHeap []*Value

func NewGossipElection(n *onet.TreeNodeInstance) (onet.ProtocolInstance, error) {
	ge := &GossipElection{
		TreeNodeInstance: n,
		stop:             make(chan bool),
		threshold:        n.Tree().Size() * 2.0 / 3.0,
		waitCh:           make(chan bool, 1),
		newValue:         make(chan *Value, n.Tree().Size()),
		valueFoundCh:     make(chan *ValueFound, 1),
		newRootValue:     make(chan *Value, len(n.Children())),
		firstTime:        true,
		list:             n.Roster(),
	}
	ge.genValue()
	log.ErrFatal(ge.RegisterHandler(ge.ReceiveValue))
	log.ErrFatal(ge.RegisterHandler(ge.ReceiveValueFound))
	log.ErrFatal(ge.RegisterHandler(ge.ReceiveFinished))
	return ge, nil
}

func (p *GossipElection) Start() error {
	p.gossip(p.value)
	log.Lvl2(p.Name(), "Root started gossip with threshold ", p.threshold)
	return nil
}

func (p *GossipElection) Dispatch() error {
	round := 0
	rootValues := newValueCounter(p.threshold)
	lowest := p.value
	timeout := make(chan bool)
	stopTimeout := make(chan bool, 1)
	var done bool
	go func() {
		for {
			select {
			case <-time.After(Timeout):
				timeout <- true
			case <-stopTimeout:
				return
			}
		}
	}()
	for {
		select {
		case <-timeout:
			round++
			log.Lvl2(p.Name(), "timeout occured round", round, "=> gossiping", lowest.Index)
			if p.IsRoot() {
				log.Lvl2("Root received", rootValues.Max().Counter, "/", p.threshold, " => reset for round", round)
				rootValues.Reset()
			}
			p.gossip(lowest)
		case v := <-p.newValue:
			if v.Less(lowest) {
				lowest = v
			}
		case valFound := <-p.valueFoundCh:
			if lowest.Index != valFound.Index {
				log.Lvl2(p.Name(), "have a different idx mine(", lowest.Index, ") vs root(", valFound.Index, ")")
			}
			p.sendValueFound(valFound)
		case v := <-p.newRootValue:
			if done {
				continue
			}
			rootValues.Put(v)
			if !rootValues.ThresholdReached() {
				continue
			}
			// found !
			log.Lvl2(p.Name(), "Received ", rootValues.Max().Counter, "values for idx", rootValues.Max().Index, "at round", round, "! Stopping!")
			val := rootValues.Max().Value
			p.foundAll(val)
			done = true
		case <-p.stop:
			log.Lvl3(p.Name(), "stopping...")
			close(stopTimeout)
			return nil
		}
	}
}

func (p *GossipElection) ReceiveValue(v ValueHandler) error {
	val := &v.Value
	n := v.TreeNode
	public := p.list.Get(int(val.Index)).Public
	if !Verify(p.Suite(), public, Msg, val.Sum, val.Proof) {
		log.Error(p.Name(), "received invalid proof from", n.Name())
		return errors.New("invalid proof")
	}

	log.Lvl3(p.Name(), "received value ", v.Value.Index, "from", v.TreeNode.Name())
	p.newValue <- val

	if p.IsRoot() {
		p.newRootValue <- val
	}
	return nil
}

func (p *GossipElection) ReceiveValueFound(v ValueFoundHandler) error {
	val := &v.ValueFound
	p.done = true
	log.Lvl2(p.Name(), "received from root found value", val.Index, "! Sending finishing..")

	p.valueFoundCh <- val
	return nil
}

func (p *GossipElection) sendValueFound(val *ValueFound) {

	//close(p.stop)

	if err := p.SendToChildren(val); err != nil {
		log.Lvl3(err)
	}

	if p.IsLeaf() {
		if err := p.SendToParent(&Finished{}); err != nil {
			log.Lvl3(err)
		}
		p.Done()
		log.Lvl2(p.Name(), "Leaf finished !")
		close(p.stop)
	}
}

func (p *GossipElection) ReceiveFinished(f FinishedHandler) error {
	p.finished++
	if p.finished < len(p.Children()) {
		return nil
	}

	if err := p.SendToParent(&Finished{}); err != nil {
		log.Lvl3(err)
	}

	if !p.IsRoot() {
		log.Lvl2(p.Name(), "node finished & send to", p.Parent().Name())
	}

	if p.IsRoot() {
		log.Lvl2(p.Name(), "root received all finished -> closing")
		close(p.waitCh)
	}
	p.Done()
	close(p.stop)
	return nil
}

func (p *GossipElection) foundAll(common *Value) {
	vf := ValueFound(*common)
	p.sendValueFound(&vf)
	if p.doneCb != nil {
		p.doneCb(common)
	}
	log.Print("Root called callback")
}

func (p *GossipElection) RegisterDoneCb(fn func(v *Value)) {
	p.doneCb = fn
}

func (p *GossipElection) WaitFinish() {
	<-p.waitCh
}

func (p *GossipElection) gossip(lowest *Value) {
	// choose random nodes
	list := p.List()
	taken := make(map[int]bool)
	for i := 0; i < NbNode; i++ {
		var n = rand.Intn(len(list))
		for taken[n] && p.Index() != n {
			n = rand.Intn(len(list))
		}
		taken[n] = true
		//log.Print(p.Name(), "sending to", list[n].Name(), " for idx ", p.lowest.Index)
		if err := p.SendTo(list[n], lowest); err != nil {
			log.Error(p.Name(), "err gossiping to", list[n].Name(), err)
		}
	}

	// send to root
	p.SendTo(p.Root(), lowest)
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

type ValueHandler struct {
	*onet.TreeNode
	Value
}

type ValueFoundHandler struct {
	*onet.TreeNode
	ValueFound
}

type Finished struct{}

type FinishedHandler struct {
	*onet.TreeNode
	Finished
}
