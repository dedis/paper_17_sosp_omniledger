// Package pbft is the Practical Byzantine Fault Tolerance algorithm with some simplifications.
package pbft

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/dedis/cothority/byzcoin/blockchain"
	"gopkg.in/dedis/crypto.v0/abstract"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
)

const (
	notFound = -1
)

// Protocol implements onet.Protocol
// we do basically the same as in http://www.pmg.lcs.mit.edu/papers/osdi99.pdf
// with the following diffs:
// there is no client/server request and reply (or first line in Figure 1)
// instead of MACs we just send around the hash of the block
// this will make the protocol faster, but the network latency will overweigh
// this skipped computation anyways
type Protocol struct {
	// the node we are represented-in
	*onet.TreeNodeInstance
	// the suite we use
	suite abstract.Suite
	// aggregated public key of the peers
	aggregatedPublic abstract.Point
	// a flat list of all TreeNodes (similar to jvss)
	nodeList []*onet.TreeNode
	// our index in the entitylist
	index int

	// we do not care for servers or clients (just store one block here)
	TrBlock *blockchain.TrBlock

	threshold int
	// channels:
	prePrepareChan chan prePrepareChan
	prepareChan    chan prepareChan
	commitChan     chan commitChan

	OnDoneCB func()

	consensusDone bool
	consensusLock sync.Mutex

	prepreStarted bool
	prepreLock    sync.Mutex

	tempPrepareLock sync.Mutex
	tempPrepareMsg  []*Prepare

	tempCommitLock sync.Mutex
	tempCommitMsg  []*Commit

	finishChan chan finishChan
}

const (
	statePrePrepare = iota
	statePrepare
	stateCommit
	stateFinished
)

// NewProtocol returns a new pbft protocol
func NewProtocol(n *onet.TreeNodeInstance) (*Protocol, error) {
	pbft := new(Protocol)
	tree := n.Tree()
	pbft.TreeNodeInstance = n
	pbft.nodeList = tree.List()
	idx := notFound
	for i, tn := range pbft.nodeList {
		if tn.ID.Equal(n.TreeNode().ID) {
			idx = i
		}
	}
	if idx == notFound {
		panic(fmt.Sprintf("Could not find ourselves %+v in the list of nodes %+v", n, pbft.nodeList))
	}
	pbft.index = idx
	// 2/3 * #participants == threshold FIXME the threshold is actually XXX
	pbft.threshold = int(math.Ceil(float64(len(pbft.nodeList)) * 2.0 / 3.0))

	if err := n.RegisterChannel(&pbft.prePrepareChan); err != nil {
		return pbft, err
	}
	if err := n.RegisterChannel(&pbft.prepareChan); err != nil {
		return pbft, err
	}
	if err := n.RegisterChannel(&pbft.commitChan); err != nil {
		return pbft, err
	}
	if err := n.RegisterChannel(&pbft.finishChan); err != nil {
		return pbft, err
	}

	return pbft, nil
}

// Dispatch implements onet.Protocol (and listens on all message channels)
func (p *Protocol) Dispatch() error {
	for {
		select {
		case msg := <-p.prePrepareChan:
			p.handlePrePrepare(&msg.PrePrepare)
		case msg := <-p.prepareChan:
			p.handlePrepare(&msg.Prepare)
		case msg := <-p.commitChan:
			p.handleCommit(&msg.Commit)
		case <-p.finishChan:
			log.Lvl3(p.Name(), "Got Done Message ! FINISH")
			p.Done()
			return nil
		}
	}
}

// Start implements the ProtocolInstance interface of onet.
func (p *Protocol) Start() error {
	return p.PrePrepare()
}

func (p *Protocol) PrePreSet() {
	p.prepreLock.Lock()
	defer p.prepreLock.Unlock()
	p.prepreStarted = true
}

func (p *Protocol) PrePrepareDone() bool {
	p.prepreLock.Lock()
	defer p.prepreLock.Unlock()
	return p.prepreStarted
}

// PrePrepare intializes a full run of the protocol.
func (p *Protocol) PrePrepare() error {
	// pre-prepare: broadcast the block
	var err error
	log.Lvl2(p.Number(), "Broadcast PrePrepare")
	prep := &PrePrepare{p.TrBlock}
	if err := p.Broadcast(prep); err != nil {
		log.Error(p.Name(), "sending pre-prepare:", err)
	}
	log.Lvl3(p.Name(), "Broadcast PrePrepare DONE")
	p.handlePrePrepare(prep)
	return err
}

func (p *Protocol) Number() string {
	return fmt.Sprintf("%d/%d", p.Index(), len(p.nodeList))
}

// handlePrePrepare receive preprepare messages and go to Prepare if it received
// enough.
func (p *Protocol) handlePrePrepare(prePre *PrePrepare) {
	// prepare: verify the structure of the block and broadcast
	// prepare msg (with header hash of the block)
	log.Lvl3(p.Name(), "handlePrePrepare() ")
	if !verifyBlock(prePre.TrBlock, "", "") {
		log.Fatal(p.Name(), "Block couldn't be verified")
		return
	}

	log.Lvl2(p.Number(), "Block valid:  pre-prepare => prepare")
	prep := &Prepare{prePre.TrBlock.HeaderHash}
	p.PrepareAdd(prep)
	p.PrePreSet()
	if err := p.Broadcast(prep); err != nil {
		log.Error(p.Name(), "Error broadcasting Prepare")
	}
}

func (p *Protocol) PrepareLen() int {
	p.tempPrepareLock.Lock()
	defer p.tempPrepareLock.Unlock()
	return len(p.tempPrepareMsg)
}

func (p *Protocol) PrepareAdd(pre *Prepare) {
	p.tempPrepareLock.Lock()
	defer p.tempPrepareLock.Unlock()
	p.tempPrepareMsg = append(p.tempPrepareMsg, pre)
}

func (p *Protocol) Threshold() int {
	var localThreshold = p.threshold
	// we dont have a "client", the root DONT send any prepare message
	// so for the rest of the nodes the threshold is less one.
	if !p.IsRoot() {
		localThreshold--
	}
	return localThreshold
}

func (p *Protocol) handlePrepare(pre *Prepare) {
	p.PrepareAdd(pre)
	if !p.PrePrepareDone() {
		log.Lvl3(p.Name(), "handlePrepare(): no PRE-PREPARE yet :(")
		return
	}

	if p.PrepareLen() < p.Threshold() {
		log.Lvl3("%s: Prepare threshold not reached: %d/%d", p.Name(), p.PrepareLen(), p.Threshold())
		return
	}

	// TRANSITION PREPARE => COMMIT
	log.Lvl2(p.Number(), "Threshold (", p.Threshold(), ") reached: broadcast Commit")
	com := &Commit{pre.HeaderHash}
	p.CommitAdd(com)
	if err := p.Broadcast(com); err != nil {
		log.Error(p.Name(), "Error broadcasting Commit:", err)

	}
}

func (p *Protocol) CommitLen() int {
	p.tempCommitLock.Lock()
	defer p.tempCommitLock.Unlock()
	return len(p.tempCommitMsg)
}

func (p *Protocol) CommitAdd(c *Commit) {
	p.tempCommitLock.Lock()
	defer p.tempCommitLock.Unlock()
	p.tempCommitMsg = append(p.tempCommitMsg, c)
}

func (p *Protocol) ConsensusDone() bool {
	p.consensusLock.Lock()
	defer p.consensusLock.Unlock()
	return p.consensusDone
}

func (p *Protocol) ConsensusSet() {
	p.consensusLock.Lock()
	defer p.consensusLock.Unlock()
	p.consensusDone = true
}

// handleCommit receives commit messages and signal the end if it received
// enough of it.
func (p *Protocol) handleCommit(com *Commit) {
	p.CommitAdd(com)
	if p.PrepareLen() < p.Threshold() {
		log.Lvl3(p.Name(), "Not enough Prepare =>  Storing commit packet")
		return
	}

	if p.CommitLen() < p.Threshold() {
		log.Lvl3("%s: Commit threshold NOT REACHED", p.Name())
		return
	}

	if p.ConsensusDone() {
		return
	}
	p.ConsensusSet()
	log.Lvl1(p.Name(), "CONSENSUS reached !")
	if p.IsRoot() && p.OnDoneCB != nil {
		log.Lvl1(p.Name(), " ROOT Calling consensus Callback")
		p.OnDoneCB()
		p.finish()
	}
	return
}

// finish is called by the root to tell everyone the root is done
func (p *Protocol) finish() {
	f := &Finish{"Finish"}
	if err := p.Broadcast(f); err != nil {
		log.Lvl3(p.Name(), "root can't broadcast FINISH", err)
		return
	}
	// notify ourself
	go func() { p.finishChan <- finishChan{nil, Finish{}} }()
}

// verifyBlock is a simulation of a real block verification algorithm
// FIXME merge with Nicolas' code (public method in byzcoin)
func verifyBlock(block *blockchain.TrBlock, lastBlock, lastKeyBlock string) bool {
	//We measure the average block verification delays is 174ms for an average
	//block of 500kB.
	//To simulate the verification cost of bigger blocks we multiply 174ms
	//times the size/500*1024
	b, _ := json.Marshal(block)
	s := len(b)
	var n time.Duration
	n = time.Duration(s / (500 * 1024))
	time.Sleep(150 * time.Millisecond * n) //verification of 174ms per 500KB simulated
	// verification of the header
	verified := block.Header.Parent == lastBlock && block.Header.ParentKey == lastKeyBlock
	verified = verified && block.Header.MerkleRoot == blockchain.HashRootTransactions(block.TransactionList)
	verified = verified && block.HeaderHash == blockchain.HashHeader(block.Header)

	return verified
}
