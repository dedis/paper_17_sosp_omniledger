package main

import (
	"time"

	"github.com/BurntSushi/toml"
	"github.com/dedis/paper_17_sosp_omniledger/state/skipchain"
	"gopkg.in/dedis/onet.v1"
	"gopkg.in/dedis/onet.v1/app"
	"gopkg.in/dedis/onet.v1/log"
	"gopkg.in/dedis/onet.v1/network"
	"gopkg.in/dedis/onet.v1/simul"
	"gopkg.in/dedis/onet.v1/simul/monitor"
)

func init() {
	onet.SimulationRegister("OmniState", NewSimulationProtocol)
	network.RegisterMessage(OmniBlockState{})
	network.RegisterMessage(OmniBlockStateConfig{})
	network.RegisterMessage(OmniBlockTrans{})
}

// SimulationProtocol implements onet.Simulation.
type SimulationProtocol struct {
	onet.SimulationBFTree
	FileBlock      string
	FileUnspent    string
	BlocksPerDay   int
	SimulationDays int
	StateBlockFreq int
	Scaling        int64
	TimeStart      int
	TimeEnd        int
	TimeStep       int
}

// NewSimulationProtocol is used internally to register the simulation (see the init()
// function above).
func NewSimulationProtocol(config string) (onet.Simulation, error) {
	es := &SimulationProtocol{}
	_, err := toml.Decode(config, es)
	if err != nil {
		return nil, err
	}
	return es, nil
}

// Setup implements onet.Simulation.
func (s *SimulationProtocol) Setup(dir string, hosts []string) (
	*onet.SimulationConfig, error) {
	if s.BlocksPerDay <= 0 ||
		s.FileUnspent == "" ||
		s.FileBlock == "" {
		log.Fatal("Not correct input-parameters.")
	}
	sc := &onet.SimulationConfig{}
	s.CreateRoster(sc, hosts, 2000)
	err := s.CreateTree(sc)
	if err != nil {
		return nil, err
	}
	log.ErrFatal(app.Copy(dir, s.FileBlock))
	log.ErrFatal(app.Copy(dir, s.FileUnspent))
	return sc, nil
}

// Node can be used to initialize each node before it will be run
// by the server. Here we call the 'Node'-method of the
// SimulationBFTree structure which will load the roster- and the
// tree-structure to speed up the first round.
func (s *SimulationProtocol) Node(config *onet.SimulationConfig) error {
	index, _ := config.Roster.Search(config.Server.ServerIdentity.ID)
	if index < 0 {
		log.Fatal("Didn't find this node in roster")
	}
	log.Lvl3("Initializing node-index", index)
	return s.SimulationBFTree.Node(config)
}

// Run implements onet.Simulation.
func (s *SimulationProtocol) Run(config *onet.SimulationConfig) error {
	size := config.Tree.Size()
	log.Lvl2("Size is:", size, "rounds:", s.Rounds)
	// Initialize skipchain
	trans, err := readCSV(s.FileBlock, s.BlocksPerDay, 1e6, true)
	log.ErrFatal(err)
	state, err := readCSV(s.FileUnspent, s.BlocksPerDay, 37, false)
	log.ErrFatal(err)
	initSkip := monitor.NewTimeMeasure("init_skip")
	sbClient := skipchain.NewClient()

	// Setting up the transaction-skipchain
	sbTrans, cerr := sbClient.CreateGenesis(config.Roster, 1, 1,
		skipchain.VerificationStandard, nil, nil)
	log.ErrFatal(cerr)
	replyTrans := &skipchain.StoreSkipBlockReply{nil, sbTrans}

	// Setting up the state-skipchain
	sbState, cerr := sbClient.CreateGenesis(config.Roster, 1, 1,
		skipchain.VerificationStandard, nil, nil)
	log.ErrFatal(cerr)
	replyState := &skipchain.StoreSkipBlockReply{nil, sbState}

	// Setting up the state-skipchain-configuration
	sbStateConfig, cerr := sbClient.CreateGenesis(config.Roster, 3, 3,
		skipchain.VerificationStandard, nil, nil)
	log.ErrFatal(cerr)
	replyStateConfig := &skipchain.StoreSkipBlockReply{nil, sbStateConfig}

	start := trans.first
	if start < state.first {
		start = state.first
	}
	start *= s.BlocksPerDay
	stop := len(trans.values)
	if stop > len(state.values) {
		stop = len(state.values)
	}
	stop = (stop - 1) * s.BlocksPerDay
	simulationStart := stop - s.SimulationDays*s.BlocksPerDay
	if simulationStart < start {
		simulationStart = start
	}
	lastTransSize := trans.GetValue(simulationStart)
	lastTransWithState := lastTransSize
	startStateSize := int64(0)
	var obTransList []*OmniBlockTrans
	//startStateSize := state.GetValue(simulationStart)
	last := time.Now()
	for count := simulationStart; count < stop; count++ {
		nowTransSize := trans.GetValue(count)
		sbTime := float32(count-simulationStart) / float32(s.BlocksPerDay)

		// Add a state-block at the very beginning.
		addState := count == simulationStart
		currState := state.GetValue(count) - startStateSize
		if s.StateBlockFreq > 0 {
			if (count-simulationStart)%(s.BlocksPerDay*s.StateBlockFreq) == 0 {
				addState = true
			}
		} else {
			if nowTransSize-lastTransWithState > currState {
				addState = true
			}
		}

		if addState {
			size := currState / s.Scaling
			obs := &OmniBlockState{
				SBTransaction: replyTrans.Latest.Hash,
				State:         make([]byte, size),
				Time:          sbTime,
			}
			t := time.Now()
			replyState, cerr = sbClient.StoreSkipBlock(replyState.Latest, nil, obs)
			log.ErrFatal(cerr)
			log.LLvlf2("Added state-block with size: %dkB in %s at %f",
				size*s.Scaling/1e3, time.Now().Sub(t), sbTime)
			obsc := &OmniBlockStateConfig{
				SBState:       replyState.Latest.Hash,
				SBTransaction: replyTrans.Latest.Hash,
				Time:          sbTime,
			}
			replyStateConfig, cerr = sbClient.StoreSkipBlock(replyStateConfig.Latest, nil, obsc)
			log.ErrFatal(cerr)
			lastTransWithState = nowTransSize
		}

		if count%s.BlocksPerDay == 0 {
			log.LLvlf2("Day: %d", (count-simulationStart)/s.BlocksPerDay)
		}
		transSize := (nowTransSize - lastTransSize) / s.Scaling
		lastTransSize = nowTransSize
		obt := &OmniBlockTrans{
			SBStateConfig: replyStateConfig.Latest.Hash,
			SBTrans:       make([]byte, transSize),
			Time:          sbTime,
		}
		replyTrans, cerr = sbClient.StoreSkipBlock(replyTrans.Latest, nil, obt)
		log.ErrFatal(cerr)
		obTransList = append(obTransList, &OmniBlockTrans{
			SBStateConfig: replyStateConfig.Latest.Hash,
			SBState:       replyState.Latest.Hash,
			SBTrans:       replyTrans.Latest.Hash,
			Time:          sbTime,
		})
		now := time.Now()
		if count%10 == 0 {
			log.LLvlf3("Stored %d/%d transaction-blocks in %s - size: %dkB",
				count-simulationStart, stop-simulationStart,
				now.Sub(last), transSize*s.Scaling/1e3)
		}
		last = now
	}

	//stConfList, cerr := sbClient.GetUpdateChain(config.Roster, sbStateConfig.Hash)
	log.ErrFatal(err)
	initSkip.Record()
	time.Sleep(time.Second)
	for backDay := s.TimeStart; backDay < s.TimeEnd; backDay += s.TimeStep {
		monitor.RecordSingleMeasure("back_day", float64(backDay))
		log.Lvl1("Measuring time", backDay)
		log.Lvl2("Getting latest state and transactions", backDay, "days back")
		startTrans := obTransList[0]
		startBlock, cerr := sbClient.GetSingleBlock(config.Roster, startTrans.SBTrans)
		stateBlock, cerr := sbClient.GetSingleBlock(config.Roster,
			startTrans.SBStateConfig)
		log.ErrFatal(cerr)

		// Start with the bitcoin-method of taking all blocks
		time_bc := monitor.NewTimeMeasure("time_bitcoin")
		bw_bc := monitor.NewCounterIOMeasure("bw_bitcoin", sbClient)

		bitcoinBlock := startBlock
		bitcoinBlockCount := 1
		for {
			_, obtInt, err := network.Unmarshal(bitcoinBlock.Data)
			log.ErrFatal(err)
			obt := obtInt.(*OmniBlockTrans)
			if obt.Time > float32(backDay) {
				break
			}
			bitcoinBlock, cerr = sbClient.GetSingleBlock(config.Roster,
				bitcoinBlock.ForwardLink[0].Hash)
			log.ErrFatal(cerr)
			bitcoinBlockCount++
		}
		monitor.RecordSingleMeasure("transblocks_bitcoin",
			float64(bitcoinBlockCount))
		bw_bc.Record()
		time_bc.Record()

		// Now measure the omniledger-way of doing things.
		time_ol := monitor.NewTimeMeasure("time_omniledger")
		bw_ol := monitor.NewCounterIOMeasure("bw_omniledger", sbClient)

		omniBlock := startBlock
		omniBlockCount := 1
		log.ErrFatal(err)
		nextStateBlock, cerr := sbClient.GetSingleBlock(config.Roster,
			stateBlock.ForwardLink[0].Hash)
		_, nsb, err := network.Unmarshal(nextStateBlock.Data)
		log.ErrFatal(err)
		nextStateBlockTime := nsb.(*OmniBlockStateConfig).Time
		for {
			_, obtInt, err := network.Unmarshal(omniBlock.Data)
			log.ErrFatal(err)
			obt := obtInt.(*OmniBlockTrans)
			if obt.Time > float32(backDay) {
				break
			}
			if obt.Time > nextStateBlockTime {
				_, cerr := sbClient.GetSingleBlock(config.Roster,
					obTransList[1].SBState)
				log.ErrFatal(cerr)
				break
			}
			omniBlock, cerr = sbClient.GetSingleBlock(config.Roster,
				omniBlock.ForwardLink[0].Hash)
			log.ErrFatal(cerr)
			omniBlockCount++
		}
		monitor.RecordSingleMeasure("transblocks_omniledger",
			float64(omniBlockCount))

		bw_ol.Record()
		time_ol.Record()
	}
	return nil
}

type OmniBlockState struct {
	SBTransaction skipchain.SkipBlockID
	State         []byte
	Time          float32
}

type OmniBlockStateConfig struct {
	SBState       skipchain.SkipBlockID
	SBTransaction skipchain.SkipBlockID
	Time          float32
}

type OmniBlockTrans struct {
	SBStateConfig skipchain.SkipBlockID
	SBState       skipchain.SkipBlockID
	SBTrans       skipchain.SkipBlockID
	Time          float32
}

func main() {
	simul.Start()
}
