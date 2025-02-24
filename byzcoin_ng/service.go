package main

import (
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol/blockchain"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/service"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/simul/monitor"
)

/*
 * Defines the simulation for the service-template to be run with
 * `cothority/simul`.
 */

func init() {
	onet.SimulationRegister("ServiceBNG", NewSimulation)
}

// Simulation only holds the BFTree simulation
type simulation struct {
	onet.SimulationBFTree
	// your simulation specific fields:
	Blocksize int
	lock      sync.Mutex
	Threads   int
}

// NewSimulation returns the new simulation, where all fields are
// initialised using the config-file
func NewSimulation(config string) (onet.Simulation, error) {
	es := &simulation{}
	_, err := toml.Decode(config, es)
	if err != nil {
		return nil, err
	}
	return es, nil
}

// Setup creates the tree used for that simulation
func (e *simulation) Setup(dir string, hosts []string) (
	*onet.SimulationConfig, error) {
	err := blockchain.EnsureBlockIsAvailable(dir)
	if err != nil {
		log.Fatal("Couldn't get block:", err)
	}

	sc := &onet.SimulationConfig{}
	e.CreateRoster(sc, hosts, 2000)
	err = e.CreateTree(sc)
	if err != nil {
		return nil, err
	}
	return sc, nil
}

// Run is used on the destination machines and runs a number of
// rounds
func (e *simulation) Run(config *onet.SimulationConfig) error {
	//size := config.Tree.Size()
	log.Print(config.Tree.Root.Children)
	service, ok := config.GetService(byzcoin_ng.ServiceName).(*byzcoin_ng.Service)
	if service == nil || !ok {
		log.Fatal("Didn't find service", byzcoin_ng.ServiceName)
	}
	err := service.StartSimul(blockchain.GetBlockDir(), e.Blocksize, config.Roster)
	if err != nil {
		log.Error(err)
	}
	log.Lvl1("Size is:", e.Blocksize, "rounds:", e.Rounds)
	var wg sync.WaitGroup
	round1 := monitor.NewTimeMeasure("round")
	for i := 0; i < e.Threads; i++ {
		wg.Add(1)
		go func(j int) {
			for {
				e.lock.Lock()
				if e.Rounds > 0 {
					e.Rounds--
					round := e.Rounds
					e.lock.Unlock()
					lat := monitor.NewTimeMeasure("lat")
					log.Lvl1("Starting round", round, "at thread", j)
					_, err := service.StartEpoch(round, e.Blocksize)
					if err != nil {
						log.Error("problem after epoch")
					}
					lat.Record()
				} else {
					e.lock.Unlock()
					break
				}
			}
			wg.Done()
		}(i)
		time.Sleep(1000 * time.Millisecond)

		//Propagation is not needed but bftcosi does not save the block
		//service.startPropagation(block)
	}
	wg.Wait()
	round1.Record()

	log.Lvl2("done with measures")

	return nil
}
