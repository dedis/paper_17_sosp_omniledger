package main

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/simul/monitor"
	"github.com/dedis/paper_17_sosp_omniledger/gossip"
)

func init() {
	onet.SimulationRegister("Gossip", NewSimulation)
}

type GossipSimulation struct {
	onet.SimulationBFTree
	Timeout      int
	NbNodeGossip int
}

func NewSimulation(config string) (onet.Simulation, error) {
	s := &GossipSimulation{}
	_, err := toml.Decode(config, s)
	return s, err
}

func (s *GossipSimulation) Setup(dir string, hosts []string) (*onet.SimulationConfig, error) {
	sc := &onet.SimulationConfig{}
	s.CreateRoster(sc, hosts, 2000)
	err := s.CreateTree(sc)
	return sc, err
}

func (s *GossipSimulation) Node(sc *onet.SimulationConfig) error {
	gossip.Timeout = time.Millisecond * time.Duration(s.Timeout)
	gossip.NbNode = s.NbNodeGossip
	return nil
}

func (s *GossipSimulation) Run(config *onet.SimulationConfig) error {
	done := make(chan gossip.Value)
	cb := func(v *gossip.Value) {
		done <- *v
	}

	log.Print("Starting gossip with", s.Hosts, "hosts")

	for round := 0; round < s.Rounds; round++ {
		p, err := config.Overlay.CreateProtocol("Gossip", config.Tree, onet.NilServiceID)
		log.ErrFatal(err)

		gossip := p.(*gossip.GossipElection)
		gossip.RegisterDoneCb(cb)

		log.Print("\t Round", round, " (", s.Hosts, "hosts)")
		roundM := monitor.NewTimeMeasure("gossip")
		go gossip.Start()

		select {
		case _ = <-done:
			roundM.Record()
			log.Lvl1("Finished round ", round)
		case <-time.After(3 * time.Minute):
			roundM.Record()
			return fmt.Errorf("round %d did not return", round)
		}
	}
	return nil
}
