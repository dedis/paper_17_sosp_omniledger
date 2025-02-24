package main

import (
	"errors"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/protocol/blockchain"
	"github.com/dedis/paper_17_sosp_omniledger/byzcoin_lib/service"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"
	"gopkg.in/dedis/onet.v1/simul/monitor"
)

/*
 * Defines the simulation for the service-template to be run with
 * `cothority/simul`.
 */

func init() {
	onet.SimulationRegister("Service2BNG", NewSimulation)
}

var magicNum = [4]byte{0xF9, 0xBE, 0xB4, 0xD9}

// Simulation only holds the BFTree simulation
type simulation struct {
	onet.SimulationBFTree
	// your simulation specific fields:
	Blocksize    int
	lock         sync.Mutex
	Threads      int
	Shards       int
	Shard_length int
	Auditors     int
	audit_roster *onet.Roster
}

// NewSimulation r the new simulation, where all fields are
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
func (s *simulation) Setup(dir string, hosts []string) (
	*onet.SimulationConfig, error) {
	err := blockchain.EnsureBlockIsAvailable(dir)
	if err != nil {
		log.Fatal("Couldn't get block:", err)
	}

	sc := &onet.SimulationConfig{}
	s.CreateRoster(sc, hosts, 2000)
	err = s.CreateTree(sc) //useless?
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *simulation) Node(sc *onet.SimulationConfig) error {
	i, _ := sc.Roster.Search(sc.Server.ServerIdentity.ID)
	if s.Auditors > 0 { //leader of auditors is 0
		s.audit_roster = onet.NewRoster(sc.Roster.List[0 : s.Auditors-1])
		s.Shard_length = (len(sc.Roster.List) - s.Auditors) / s.Shards
		//Create Shards
		//log.Lvl1("i", i, "shard lenth", s.Shard_length, "full", len(sc.Roster.List))
		if i >= s.Auditors && (i-s.Auditors)%s.Shard_length == 0 && i+s.Shard_length <= len(sc.Roster.List) {
			roster := onet.NewRoster(sc.Roster.List[i : i+s.Shard_length-1])
			log.Lvl1("leader is:", i, "last is:", i+s.Shard_length-1)
			go s.run_service(sc, roster, i)

		}
	} else {
		s.Shard_length = (len(sc.Roster.List) - 1) / s.Shards
		if i%s.Shard_length == 1 && i+s.Shard_length <= len(sc.Roster.List) {
			roster := onet.NewRoster(sc.Roster.List[i : i+s.Shard_length-1])
			log.Lvl1("leader is:", i, "last is:", i+s.Shard_length-1)
			go s.run_service(sc, roster, i)
		}

	}
	return nil
}

// Run is used on the destination machines and runs a number of
// rounds
func (s *simulation) Run(config *onet.SimulationConfig) error {
	service, ok := config.GetService(byzcoin_ng.ServiceName).(*byzcoin_ng.Service)
	if service == nil || !ok {
		log.Fatal("Didn't find service", byzcoin_ng.ServiceName)
	}
	parser, err := blockchain.NewParser(blockchain.GetBlockDir(), magicNum)
	transactions, err := parser.Parse(0, 50)
	if len(transactions) == 0 {
		return errors.New("Couldn't read any transactions.")
	}
	if err != nil {
		log.Error("Error: Couldn't parse blocks in", blockchain.GetBlockDir(),
			".\nPlease download bitcoin blocks as .dat files first and place them in",
			blockchain.GetBlockDir(), "Either run a bitcoin node (recommended) or using a torrent.")
		return err
	}
	//time.Sleep(30 * time.Second)
	var cl *monitor.TimeMeasure
	for j := 0; j < 10; j++ {
		tr := transactions[j]
		log.Lvl1("client parsed transaction", tr)
		if j > 0 {
			cl = monitor.NewTimeMeasure("client")
		}
		var wg sync.WaitGroup
		var audit_lock sync.Mutex
		var audit_slice []byzcoin_ng.Reply
		//var audit_slice = make([]byzcoin_ng.Reply, (len(config.Roster.List)-s.Auditors)/s.Shard_length)
		//var audit_slice [(len(config.Roster.List) - s.Auditors) / s.Shard_length]byzcoin_ng.Reply
		log.Lvl3("slice has", (len(config.Roster.List)-s.Auditors)/s.Shard_length, s.Auditors)
		for i, node := range config.Roster.List {
			if s.Auditors > 0 {
				if i >= s.Auditors {
					if (i-s.Auditors)%s.Shard_length == 0 && i+s.Shard_length <= len(config.Roster.List) {
						log.Lvl1("Client sending to node with audit", i)
						wg.Add(1)
						go func(node *network.ServerIdentity, j int) error {
							ret := &byzcoin_ng.Reply{}
							req := &byzcoin_ng.Request{tr}
							cerr := byzcoin_ng.NewClient().SendProtobuf(node, req, ret)
							log.ErrFatal(cerr)
							audit_lock.Lock()
							audit_slice = append(audit_slice, *ret)
							audit_lock.Unlock()
							//log.Lvl1("got answer placing at", j, (j-s.Auditors)/s.Shard_length, ret)
							wg.Done()
							return nil
						}(node, i)
					}
				}

			} else if i%s.Shard_length == 1 && i+s.Shard_length <= len(config.Roster.List) {

				log.Lvl1("No audit, Client sending to node", i)
				wg.Add(1)
				go func(node *network.ServerIdentity) error {
					ret := &byzcoin_ng.Reply{}
					req := &byzcoin_ng.Request{tr}
					cerr := byzcoin_ng.NewClient().SendProtobuf(node, req, ret)
					log.ErrFatal(cerr)
					err = ret.Sig.Verify(network.Suite, ret.Roster.Publics())
					if err != nil {
						log.Lvl1("cannot verify block")
						return err
					} else {
						log.Lvl1("got answer from", node)
					}
					wg.Done()
					return nil
				}(node)
			}

		}
		wg.Wait()
		if s.Auditors > 0 {
			audit_struct := &byzcoin_ng.Audit{
				HeaderHash: audit_slice[0].HeaderHash,
				Replies:    audit_slice,
				Roster:     s.audit_roster}
			log.Lvl1(audit_slice)
			service.StartAuditSignature(audit_struct)
			err = audit_struct.Sig.Verify(network.Suite, audit_struct.Roster.Publics())
			if err != nil {
				log.Lvl1("cannot verify block")
				return err
			}

		}
		if j > 0 {
			cl.Record()
		}
	}
	log.Lvl1("client is done")

	return nil
}

func (s *simulation) run_service(sc *onet.SimulationConfig, roster *onet.Roster, l int) {
	//time.Sleep(90 * time.Second)
	service, ok := sc.GetService(byzcoin_ng.ServiceName).(*byzcoin_ng.Service)
	if service == nil || !ok {
		log.Fatal("Didn't find service", byzcoin_ng.ServiceName)
	}
	err := service.StartSimul(blockchain.GetBlockDir(), s.Blocksize, roster)
	if err != nil {
		log.Error(err)
	}
	log.Lvl1("Size is:", s.Blocksize, "rounds:", s.Rounds)
	var wg sync.WaitGroup
	//round1 := monitor.NewTimeMeasure("round")
	for i := 0; i < s.Threads; i++ {
		wg.Add(1)
		go func(j int) {
			for {
				s.lock.Lock()
				if s.Rounds > 0 {
					s.Rounds--
					round := s.Rounds
					s.lock.Unlock()
					//lat := monitor.NewTimeMeasure("lat")
					log.Lvl1("Starting round", round, "at thread", j, "with leader", l, "and last node", l+s.Shard_length-1)
					_, err := service.StartEpoch(round, s.Blocksize)
					if err != nil {
						log.Lvl1("problem after epoch", err)
					}
					//lat.Record()
				} else {
					s.lock.Unlock()
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
	//round1.Record()

	log.Lvl2("done with measures")

	return
}
